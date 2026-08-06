package hec

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
)

const (
	JobRootDir           = "/var/lib/hec/jobs"
	JobKeyDir            = "/var/lib/hec/job-keys"
	JobBinaryPath        = "/opt/hec/current/bin/hec"
	JobHandlePrefix      = "job:"
	JobUnitPrefix        = "hec-job-"
	JobUnitSuffix        = ".service"
	JobIDRandomBytes     = 16
	DefaultJobOutputSize = int64(262144)
	MaxJobOutputSize     = int64(1 << 20)
)

const (
	JobStatusStarting  = "starting"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusTimedOut  = "timed_out"
	JobStatusCancelled = "cancelled"
	JobStatusUnknown   = "unknown"
)

var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

type jobSpec struct {
	Command   *string           `json:"command,omitempty"`
	Argv      []string          `json:"argv,omitempty"`
	CWD       string            `json:"cwd"`
	Env       map[string]string `json:"env,omitempty"`
	UnsetEnv  []string          `json:"unset_env,omitempty"`
	StdinPath string            `json:"stdin_path,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
}

type jobFinalResult struct {
	Status   string  `json:"status"`
	ExitCode *int    `json:"exit_code"`
	Signal   *string `json:"signal"`
	TimedOut bool    `json:"timed_out"`
	Complete bool    `json:"complete"`
}

type jobInfo struct {
	ID            string
	Handle        string
	Unit          string
	JobStatus     string
	ActiveState   string
	SubState      string
	SystemdResult string
	ExitCode      *int
	Signal        *string
	TimedOut      *bool
	StdoutBytes   int64
	StderrBytes   int64
	Directory     bool
	UnitPresent   bool
}

func generateJobID(reader io.Reader) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	raw := make([]byte, JobIDRandomBytes)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate job ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newJobID() (string, error) {
	return generateJobID(rand.Reader)
}

func parseJobHandle(handle string) (string, error) {
	if !strings.HasPrefix(handle, JobHandlePrefix) {
		return "", errors.New("handle must use the job:<id> form")
	}
	id := strings.TrimPrefix(handle, JobHandlePrefix)
	if !jobIDPattern.MatchString(id) {
		return "", errors.New("handle contains an invalid job ID")
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(raw) != JobIDRandomBytes {
		return "", errors.New("handle contains an invalid job ID")
	}
	return id, nil
}

func jobHandle(id string) string {
	return JobHandlePrefix + id
}

func jobUnit(id string) string {
	return JobUnitPrefix + id + JobUnitSuffix
}

func jobKeyDigest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func decodeJobSpec(path string) (jobSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return jobSpec{}, err
	}
	defer file.Close()

	var spec jobSpec
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return jobSpec{}, fmt.Errorf("decode job specification: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return jobSpec{}, err
	}
	if err := validateJobSpec(&spec); err != nil {
		return jobSpec{}, err
	}
	return spec, nil
}

func validateJobSpec(spec *jobSpec) error {
	if spec == nil {
		return errors.New("job specification is required")
	}
	hasCommand := spec.Command != nil
	hasArgv := spec.Argv != nil
	if hasCommand == hasArgv {
		return errors.New("job specification requires exactly one of command or argv")
	}
	if hasArgv && (len(spec.Argv) == 0 || spec.Argv[0] == "") {
		return errors.New("job argv must contain a nonempty executable name")
	}
	if spec.CWD == "" {
		spec.CWD = DefaultCWD
	}
	if _, err := durationFromMilliseconds(spec.TimeoutMS); err != nil {
		return errors.New("job timeout_ms must be greater than or equal to zero and within duration range")
	}
	if _, err := buildEnvironment(spec.Env, spec.UnsetEnv); err != nil {
		return err
	}
	if strings.ContainsRune(spec.CWD, '\x00') {
		return errors.New("job cwd contains NUL")
	}
	if strings.ContainsRune(spec.StdinPath, '\x00') {
		return errors.New("job stdin_path contains NUL")
	}
	return nil
}

func readJobFinalResult(path string) (jobFinalResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return jobFinalResult{}, err
	}
	defer file.Close()

	var result jobFinalResult
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return jobFinalResult{}, fmt.Errorf("decode job result: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return jobFinalResult{}, err
	}
	if !result.Complete {
		return jobFinalResult{}, errors.New("job result is not complete")
	}
	switch result.Status {
	case JobStatusCompleted, JobStatusFailed, JobStatusTimedOut, JobStatusCancelled:
	default:
		return jobFinalResult{}, fmt.Errorf("job result has invalid status %q", result.Status)
	}
	return result, nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return writeFileDurable(path, payload, mode)
}

func writeBytesAtomic(path string, value []byte, mode os.FileMode) error {
	return writeFileDurable(path, value, mode)
}

func writeTextAtomic(path, value string, mode os.FileMode) error {
	return writeBytesAtomic(path, []byte(value), mode)
}

func decodeStrictJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func parseLinuxSignalName(value string) (syscall.Signal, string, error) {
	name := strings.ToUpper(strings.TrimSpace(value))
	if name == "" {
		return 0, "", errors.New("signal must be a nonempty Linux signal name")
	}
	if !strings.HasPrefix(name, "SIG") {
		name = "SIG" + name
	}
	signal, ok := linuxSignalsByName[name]
	if !ok {
		return 0, "", fmt.Errorf("invalid Linux signal %q", value)
	}
	return signal, name, nil
}

var linuxSignalsByName = map[string]syscall.Signal{
	"SIGHUP":    syscall.SIGHUP,
	"SIGINT":    syscall.SIGINT,
	"SIGQUIT":   syscall.SIGQUIT,
	"SIGILL":    syscall.SIGILL,
	"SIGTRAP":   syscall.SIGTRAP,
	"SIGABRT":   syscall.SIGABRT,
	"SIGBUS":    syscall.SIGBUS,
	"SIGFPE":    syscall.SIGFPE,
	"SIGKILL":   syscall.SIGKILL,
	"SIGUSR1":   syscall.SIGUSR1,
	"SIGSEGV":   syscall.SIGSEGV,
	"SIGUSR2":   syscall.SIGUSR2,
	"SIGPIPE":   syscall.SIGPIPE,
	"SIGALRM":   syscall.SIGALRM,
	"SIGTERM":   syscall.SIGTERM,
	"SIGCHLD":   syscall.SIGCHLD,
	"SIGCONT":   syscall.SIGCONT,
	"SIGSTOP":   syscall.SIGSTOP,
	"SIGTSTP":   syscall.SIGTSTP,
	"SIGTTIN":   syscall.SIGTTIN,
	"SIGTTOU":   syscall.SIGTTOU,
	"SIGURG":    syscall.SIGURG,
	"SIGXCPU":   syscall.SIGXCPU,
	"SIGXFSZ":   syscall.SIGXFSZ,
	"SIGVTALRM": syscall.SIGVTALRM,
	"SIGPROF":   syscall.SIGPROF,
	"SIGWINCH":  syscall.SIGWINCH,
	"SIGIO":     syscall.SIGIO,
	"SIGSYS":    syscall.SIGSYS,
}
