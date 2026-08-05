package hec

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	TerminalRootDir            = "/var/lib/hec/terminals"
	TerminalRuntimeDir         = "/run/hec"
	TerminalTmuxSocket         = "/run/hec/tmux.sock"
	TerminalTmuxScope          = "hec-tmux.scope"
	TerminalHandlePrefix       = "terminal:"
	TerminalSessionPrefix      = "hec-"
	TerminalIDRandomBytes      = 16
	DefaultTerminalWidth       = 120
	DefaultTerminalHeight      = 40
	MaximumTerminalDimension   = 1000
	DefaultTerminalOutputLimit = int64(262144)
	MaximumTerminalWriteBytes  = int64(1 << 20)
	TerminalHistoryLimit       = 100000
	terminalPathEnvironment    = "/opt/hec/current/bin:/opt/hec/bin:/root/.local/bin:/root/.cargo/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

const (
	TerminalStatusStarting = "starting"
	TerminalStatusRunning  = "running"
	TerminalStatusExited   = "exited"
	TerminalStatusLost     = "lost"
)

var (
	terminalIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
	terminalEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	terminalWaitPattern    = regexp.MustCompile(`^hec-terminal-[A-Za-z0-9_-]{22}$`)
)

type terminalOpenArgs struct {
	Command  *string           `json:"command"`
	Argv     []string          `json:"argv"`
	Name     string            `json:"name"`
	CWD      string            `json:"cwd"`
	Env      map[string]string `json:"env"`
	UnsetEnv []string          `json:"unset_env"`
	Width    *int              `json:"width"`
	Height   *int              `json:"height"`
}

type terminalReadArgs struct {
	Handle string `json:"handle"`
	Mode   string `json:"mode"`
	Offset *int64 `json:"offset"`
	Limit  *int64 `json:"limit"`
}

type terminalWriteArgs struct {
	Handle     string  `json:"handle"`
	Data       *string `json:"data"`
	DataBase64 *string `json:"data_base64"`
}

type terminalResizeArgs struct {
	Handle string `json:"handle"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type terminalSignalArgs struct {
	Handle string `json:"handle"`
	Signal string `json:"signal"`
}

type terminalHandleArgs struct {
	Handle string `json:"handle"`
}

type TerminalMetadata struct {
	ID        string `json:"id"`
	Handle    string `json:"handle"`
	Name      string `json:"name,omitempty"`
	Session   string `json:"session"`
	CreatedAt string `json:"created_at"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type terminalLaunchSpec struct {
	Command     *string           `json:"command,omitempty"`
	Argv        []string          `json:"argv,omitempty"`
	CWD         string            `json:"cwd"`
	Env         map[string]string `json:"env,omitempty"`
	UnsetEnv    []string          `json:"unset_env,omitempty"`
	TmuxSocket  string            `json:"tmux_socket"`
	WaitChannel string            `json:"wait_channel"`
}

type terminalPaneInfo struct {
	PanePID        int
	Dead           bool
	DeadStatus     string
	DeadSignal     string
	Width          int
	Height         int
	CursorX        int
	CursorY        int
	CurrentCommand string
	TTY            string
	TerminalStatus string
	ExitCode       *int
	Signal         *string
}

func generateTerminalID(reader io.Reader) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	raw := make([]byte, TerminalIDRandomBytes)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate terminal ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newTerminalID() (string, error) {
	return generateTerminalID(rand.Reader)
}

func terminalHandle(id string) string {
	return TerminalHandlePrefix + id
}

func terminalSessionName(id string) string {
	return TerminalSessionPrefix + id
}

func parseTerminalHandle(handle string) (string, error) {
	if !strings.HasPrefix(handle, TerminalHandlePrefix) {
		return "", errors.New("handle must use the terminal:<id> form")
	}
	id := strings.TrimPrefix(handle, TerminalHandlePrefix)
	if !terminalIDPattern.MatchString(id) {
		return "", errors.New("handle contains an invalid terminal ID")
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(raw) != TerminalIDRandomBytes {
		return "", errors.New("handle contains an invalid terminal ID")
	}
	return id, nil
}

func parseTerminalSessionName(session string) (string, error) {
	if !strings.HasPrefix(session, TerminalSessionPrefix) {
		return "", errors.New("invalid HEC terminal session name")
	}
	id := strings.TrimPrefix(session, TerminalSessionPrefix)
	if _, err := parseTerminalHandle(terminalHandle(id)); err != nil {
		return "", errors.New("invalid HEC terminal session name")
	}
	return id, nil
}

func terminalDirectory(root, id string) string {
	return filepath.Join(root, id)
}

func terminalMetadataPath(root, id string) string {
	return filepath.Join(terminalDirectory(root, id), "metadata.json")
}

func terminalOutputPath(root, id string) string {
	return filepath.Join(terminalDirectory(root, id), "output")
}

func terminalSpecPath(root, id string) string {
	return filepath.Join(terminalDirectory(root, id), ".launch.json")
}

func validateTerminalName(name string) error {
	if name == "" {
		return nil
	}
	if !utf8.ValidString(name) {
		return errors.New("name must be valid UTF-8")
	}
	length := utf8.RuneCountInString(name)
	if length < 1 || length > 128 {
		return errors.New("name must contain between 1 and 128 characters")
	}
	for _, value := range name {
		if value < 0x20 || value == 0x7f || value >= 0x80 && value <= 0x9f {
			return errors.New("name contains a terminal-control character")
		}
	}
	return nil
}

func validateTerminalDimension(value int, field string) error {
	if value < 1 || value > MaximumTerminalDimension {
		return fmt.Errorf("%s must be between 1 and %d", field, MaximumTerminalDimension)
	}
	return nil
}

func validateTerminalEnvironment(additions map[string]string, removals []string) error {
	for _, name := range removals {
		if !terminalEnvNamePattern.MatchString(name) {
			return fmt.Errorf("unset_env contains invalid environment variable name %q", name)
		}
	}
	for name, value := range additions {
		if !terminalEnvNamePattern.MatchString(name) {
			return fmt.Errorf("env contains invalid environment variable name %q", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("env value for %q contains NUL", name)
		}
	}
	return nil
}

func normalizeTerminalOpenArgs(args *terminalOpenArgs) error {
	if args == nil {
		return errors.New("terminal.open arguments are required")
	}
	hasCommand := args.Command != nil
	hasArgv := args.Argv != nil
	if hasCommand && hasArgv {
		return errors.New("terminal.open accepts only one of command or argv")
	}
	if hasCommand && *args.Command == "" {
		return errors.New("command must be a nonempty string")
	}
	if hasArgv && (len(args.Argv) == 0 || args.Argv[0] == "") {
		return errors.New("argv must contain a nonempty executable name")
	}
	if err := validateTerminalName(args.Name); err != nil {
		return err
	}
	if args.CWD == "" {
		args.CWD = DefaultCWD
	}
	resolved, err := resolveFilesystemPath(args.CWD)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}
	if !info.IsDir() {
		return errors.New("cwd must be a directory")
	}
	args.CWD = resolved
	if err := validateTerminalEnvironment(args.Env, args.UnsetEnv); err != nil {
		return err
	}
	width := DefaultTerminalWidth
	if args.Width != nil {
		width = *args.Width
	}
	height := DefaultTerminalHeight
	if args.Height != nil {
		height = *args.Height
	}
	if err := validateTerminalDimension(width, "width"); err != nil {
		return err
	}
	if err := validateTerminalDimension(height, "height"); err != nil {
		return err
	}
	args.Width = &width
	args.Height = &height
	return nil
}

func loadTerminalMetadata(root, id string) (TerminalMetadata, error) {
	var metadata TerminalMetadata
	data, err := os.ReadFile(terminalMetadataPath(root, id))
	if err != nil {
		return metadata, err
	}
	if err := decodeStrictJSON(data, &metadata); err != nil {
		return metadata, err
	}
	if metadata.ID != id || metadata.Handle != terminalHandle(id) || metadata.Session != terminalSessionName(id) {
		return metadata, errors.New("terminal metadata does not match handle")
	}
	if _, err := time.Parse(time.RFC3339Nano, metadata.CreatedAt); err != nil {
		return metadata, errors.New("terminal metadata has an invalid creation time")
	}
	if err := validateTerminalName(metadata.Name); err != nil {
		return metadata, err
	}
	if err := validateTerminalDimension(metadata.Width, "width"); err != nil {
		return metadata, err
	}
	if err := validateTerminalDimension(metadata.Height, "height"); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func decodeTerminalLaunchSpec(path string) (terminalLaunchSpec, error) {
	var spec terminalLaunchSpec
	data, err := os.ReadFile(path)
	if err != nil {
		return spec, err
	}
	if err := decodeStrictJSON(data, &spec); err != nil {
		return spec, fmt.Errorf("decode terminal launch specification: %w", err)
	}
	if err := validateTerminalLaunchSpec(&spec); err != nil {
		return spec, err
	}
	return spec, nil
}

func validateTerminalLaunchSpec(spec *terminalLaunchSpec) error {
	if spec == nil {
		return errors.New("terminal launch specification is required")
	}
	hasCommand := spec.Command != nil
	hasArgv := spec.Argv != nil
	if hasCommand && hasArgv {
		return errors.New("terminal launch specification accepts only one of command or argv")
	}
	if hasCommand && *spec.Command == "" {
		return errors.New("terminal launch command must be nonempty")
	}
	if hasArgv && (len(spec.Argv) == 0 || spec.Argv[0] == "") {
		return errors.New("terminal launch argv must contain a nonempty executable name")
	}
	if !filepath.IsAbs(spec.CWD) || strings.ContainsRune(spec.CWD, '\x00') {
		return errors.New("terminal launch cwd must be an absolute path without NUL")
	}
	if err := validateTerminalEnvironment(spec.Env, spec.UnsetEnv); err != nil {
		return err
	}
	if !filepath.IsAbs(spec.TmuxSocket) || strings.ContainsRune(spec.TmuxSocket, '\x00') {
		return errors.New("terminal launch tmux_socket must be an absolute path without NUL")
	}
	if !terminalWaitPattern.MatchString(spec.WaitChannel) {
		return errors.New("terminal launch wait_channel is invalid")
	}
	return nil
}

func buildTerminalEnvironment(term string, additions map[string]string, removals []string) ([]string, error) {
	if term == "" || strings.ContainsRune(term, '\x00') {
		return nil, errors.New("tmux did not supply a valid TERM value")
	}
	if err := validateTerminalEnvironment(additions, removals); err != nil {
		return nil, err
	}
	values := map[string]string{
		"HOME":    "/root",
		"USER":    "root",
		"LOGNAME": "root",
		"SHELL":   "/bin/bash",
		"PATH":    terminalPathEnvironment,
		"TERM":    term,
		"LANG":    "C.UTF-8",
	}
	for _, name := range removals {
		delete(values, name)
	}
	for name, value := range additions {
		values[name] = value
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	environment := make([]string, 0, len(names))
	for _, name := range names {
		environment = append(environment, name+"="+values[name])
	}
	return environment, nil
}

func normalizeTmuxSignal(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return nil
	}
	if number, err := strconv.Atoi(value); err == nil {
		name := linuxSignalName(syscall.Signal(number))
		return &name
	}
	_, name, err := parseLinuxSignalName(value)
	if err != nil {
		return nil
	}
	return &name
}
