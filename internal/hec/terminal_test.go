package hec

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testTerminalID = "AAAAAAAAAAAAAAAAAAAAAA"

func TestGenerateAndParseTerminalID(t *testing.T) {
	id, err := generateTerminalID(bytes.NewReader(make([]byte, TerminalIDRandomBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if id != testTerminalID {
		t.Fatalf("id = %q, want %q", id, testTerminalID)
	}
	if len(id) != 22 {
		t.Fatalf("id length = %d", len(id))
	}
	handle := terminalHandle(id)
	parsed, err := parseTerminalHandle(handle)
	if err != nil || parsed != id {
		t.Fatalf("parseTerminalHandle(%q) = %q, %v", handle, parsed, err)
	}
	if terminalSessionName(id) != "hec-"+id {
		t.Fatalf("unexpected session name")
	}
}

func TestParseTerminalHandleRejectsMalformedAndTraversal(t *testing.T) {
	invalid := []string{
		"", "terminal:", "terminal:../etc/passwd", "terminal:../../etc/passwd",
		"terminal:AAAAAAAAAAAAAAAAAAAAA/", "terminal:AAAAAAAAAAAAAAAAAAAAA=",
		"job:" + testTerminalID,
		"upload:0123456789abcdef0123456789abcdef",
		"artifact:0123456789abcdef0123456789abcdef",
	}
	for _, handle := range invalid {
		t.Run(handle, func(t *testing.T) {
			if _, err := parseTerminalHandle(handle); err == nil {
				t.Fatalf("accepted malformed handle %q", handle)
			}
		})
	}
}

func TestValidateTerminalName(t *testing.T) {
	for _, value := range []string{"", "shell", strings.Repeat("x", 128), "build terminal"} {
		if err := validateTerminalName(value); err != nil {
			t.Fatalf("validateTerminalName(%q): %v", value, err)
		}
	}
	for _, value := range []string{strings.Repeat("x", 129), "bad\x00name", "bad\nname", "bad\x7fname", "bad\u0085name"} {
		if err := validateTerminalName(value); err == nil {
			t.Fatalf("accepted invalid name %q", value)
		}
	}
}

func TestNormalizeTerminalOpenArgs(t *testing.T) {
	command := "printf ok"
	width := 132
	height := 43
	args := terminalOpenArgs{
		Command:  &command,
		CWD:      ".",
		Env:      map[string]string{"A": "B"},
		UnsetEnv: []string{"OLD"},
		Width:    &width,
		Height:   &height,
	}
	if err := normalizeTerminalOpenArgs(&args); err != nil {
		t.Fatal(err)
	}
	if args.CWD != "/root" {
		t.Fatalf("cwd = %q, want /root", args.CWD)
	}
	if *args.Width != 132 || *args.Height != 43 {
		t.Fatalf("dimensions = %dx%d", *args.Width, *args.Height)
	}

	argv := []string{"/bin/true"}
	both := terminalOpenArgs{Command: &command, Argv: argv}
	if err := normalizeTerminalOpenArgs(&both); err == nil {
		t.Fatal("accepted command and argv together")
	}
	emptyCommand := ""
	if err := normalizeTerminalOpenArgs(&terminalOpenArgs{Command: &emptyCommand}); err == nil {
		t.Fatal("accepted empty command")
	}
	if err := normalizeTerminalOpenArgs(&terminalOpenArgs{Argv: []string{}}); err == nil {
		t.Fatal("accepted empty argv")
	}
	if err := normalizeTerminalOpenArgs(&terminalOpenArgs{Argv: []string{""}}); err == nil {
		t.Fatal("accepted empty argv[0]")
	}
	zero := 0
	if err := normalizeTerminalOpenArgs(&terminalOpenArgs{Width: &zero}); err == nil {
		t.Fatal("accepted zero width")
	}
}

func TestTerminalEnvironmentHandling(t *testing.T) {
	environment, err := buildTerminalEnvironment("tmux-256color", map[string]string{
		"CUSTOM": "value",
		"PATH":   "/custom/bin",
	}, []string{"LANG", "USER"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		got[name] = value
	}
	if got["CUSTOM"] != "value" || got["PATH"] != "/custom/bin" || got["TERM"] != "tmux-256color" {
		t.Fatalf("environment = %#v", got)
	}
	if _, ok := got["LANG"]; ok {
		t.Fatalf("LANG was not removed: %#v", got)
	}
	if _, ok := got["USER"]; ok {
		t.Fatalf("USER was not removed: %#v", got)
	}
	if _, ok := got["OPENAI_API_KEY"]; ok {
		t.Fatal("unexpected inherited environment")
	}
	if _, err := buildTerminalEnvironment("tmux-256color", map[string]string{"BAD-NAME": "x"}, nil); err == nil {
		t.Fatal("accepted invalid environment name")
	}
	if _, err := buildTerminalEnvironment("tmux-256color", map[string]string{"A": "bad\x00value"}, nil); err == nil {
		t.Fatal("accepted NUL environment value")
	}
}

func TestTerminalMetadataEncodingAndDecoding(t *testing.T) {
	root := t.TempDir()
	directory := terminalDirectory(root, testTerminalID)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	metadata := TerminalMetadata{
		ID:        testTerminalID,
		Handle:    terminalHandle(testTerminalID),
		Name:      "unit terminal",
		Session:   terminalSessionName(testTerminalID),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Width:     120,
		Height:    40,
	}
	if err := writeJSONAtomic(terminalMetadataPath(root, testTerminalID), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadTerminalMetadata(root, testTerminalID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, metadata) {
		t.Fatalf("loaded = %#v, want %#v", loaded, metadata)
	}
	info, err := os.Stat(terminalMetadataPath(root, testTerminalID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("metadata mode = %o", info.Mode().Perm())
	}
}

func TestTerminalLaunchSpecStrictDecode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "spec.json")
	valid := `{"argv":["/bin/true"],"cwd":"/root","tmux_socket":"/tmp/tmux.sock","wait_channel":"hec-terminal-AAAAAAAAAAAAAAAAAAAAAA"}`
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTerminalLaunchSpec(path); err != nil {
		t.Fatal(err)
	}
	invalid := `{"argv":["/bin/true"],"cwd":"/root","tmux_socket":"/tmp/tmux.sock","wait_channel":"hec-terminal-AAAAAAAAAAAAAAAAAAAAAA","unknown":true}`
	if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTerminalLaunchSpec(path); err == nil {
		t.Fatal("accepted unknown launch field")
	}
}

func TestParseTerminalPaneInfoAndSignalNormalization(t *testing.T) {
	running, err := parseTerminalPaneInfo("123\t0\t\t\t132\t43\t5\t6\tbash\t/dev/pts/7")
	if err != nil {
		t.Fatal(err)
	}
	if running.Dead || running.Width != 132 || running.Height != 43 || running.CurrentCommand != "bash" {
		t.Fatalf("running pane = %#v", running)
	}
	exited, err := parseTerminalPaneInfo("123\t1\t7\t0\t80\t24\t0\t0\ttrue\t/dev/pts/7")
	if err != nil {
		t.Fatal(err)
	}
	if exited.TerminalStatus != TerminalStatusExited || exited.ExitCode == nil || *exited.ExitCode != 7 {
		t.Fatalf("exited pane = %#v", exited)
	}
	signaled, err := parseTerminalPaneInfo("123\t1\t143\t15\t80\t24\t0\t0\tsleep\t/dev/pts/7")
	if err != nil {
		t.Fatal(err)
	}
	if signaled.Signal == nil || *signaled.Signal != "SIGTERM" || signaled.ExitCode != nil {
		t.Fatalf("signaled pane = %#v", signaled)
	}
}

func TestTerminalOutputEncodingAndRange(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	payload := []byte("abcdef")
	if _, err := file.Write(payload); err != nil {
		t.Fatal(err)
	}
	data, next, total, eof, err := readRange(file, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cde" || next != 5 || total != 6 || eof {
		t.Fatalf("range = %q next=%d total=%d eof=%v", data, next, total, eof)
	}
	text, encoding := encodeOutput([]byte("valid UTF-8"))
	if text != "valid UTF-8" || encoding != "utf8" {
		t.Fatalf("UTF-8 encoding = %q %q", text, encoding)
	}
	binary := []byte{0xff, 0x00, 0x80}
	encoded, encoding := encodeOutput(binary)
	if encoding != "base64" {
		t.Fatalf("binary encoding = %q", encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !bytes.Equal(decoded, binary) {
		t.Fatalf("binary round trip = %x, %v", decoded, err)
	}
}

func TestCreateTerminalStateModesAndCleanup(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.terminalsDir = t.TempDir()
	width := 80
	height := 24
	args := terminalOpenArgs{CWD: "/root", Width: &width, Height: &height}
	id, _, specPath, err := dispatcher.createTerminalState(args)
	if err != nil {
		t.Fatal(err)
	}
	directory := terminalDirectory(dispatcher.terminalsDir, id)
	for path, want := range map[string]os.FileMode{
		directory: 0700,
		terminalMetadataPath(dispatcher.terminalsDir, id): 0600,
		terminalOutputPath(dispatcher.terminalsDir, id):   0600,
		specPath: 0600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("terminal directory was not removed: %v", err)
	}
}
