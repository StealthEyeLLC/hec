package hec

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTerminalHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_TERMINAL_HELPER") != "1" {
		return
	}
	separator := -1
	for index, value := range os.Args {
		if value == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) != separator+3 || os.Args[separator+1] != "terminal-run" {
		os.Exit(124)
	}
	os.Exit(RunTerminal(os.Args[separator+2]))
}

func TestTerminalTmuxIntegration(t *testing.T) {
	if _, err := os.Stat("/usr/bin/tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	ctx := context.Background()
	dispatcher := newTerminalIntegrationDispatcher(t, false)
	defer cleanupTerminalIntegrationDispatcher(t, dispatcher)

	t.Run("dedicated socket and default shell", func(t *testing.T) {
		handle := openIntegrationTerminal(t, dispatcher, map[string]any{"name": "default-shell", "width": 90, "height": 30})
		if _, err := os.Stat(dispatcher.tmuxSocket); err != nil {
			t.Fatalf("dedicated socket: %v", err)
		}
		listed := dispatchIntegrationOK(t, dispatcher, "terminal.list", map[string]any{})
		entry := integrationTerminalEntry(t, listed, handle)
		if entry["name"] != "default-shell" || entry["terminal_status"] != TerminalStatusRunning {
			t.Fatalf("list entry = %#v", entry)
		}
		screen := dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "screen"})
		if screen.Result["terminal_status"] != TerminalStatusRunning {
			t.Fatalf("screen result = %#v", screen.Result)
		}
		closeIntegrationTerminal(t, dispatcher, handle)
	})

	t.Run("direct argv immediate output remain on exit and ranges", func(t *testing.T) {
		handle := openIntegrationTerminal(t, dispatcher, map[string]any{
			"name": "direct-argv",
			"argv": []any{"/usr/bin/printf", "abcdefghij"},
		})
		waitIntegrationStatus(t, dispatcher, handle, TerminalStatusExited)
		first := dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "output", "offset": 0, "limit": 4})
		if got := integrationStdoutBytes(t, first); !bytes.Equal(got, []byte("abcd")) {
			t.Fatalf("first output = %q", got)
		}
		next, ok := resultInt64(first.Result["next_offset"])
		if !ok || next != 4 {
			t.Fatalf("next_offset = %#v", first.Result["next_offset"])
		}
		second := dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "output", "offset": next, "limit": 32})
		if got := integrationStdoutBytes(t, second); !bytes.Equal(got, []byte("efghij")) {
			t.Fatalf("second output = %q", got)
		}
		screen := dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "screen"})
		if screen.Result["terminal_status"] != TerminalStatusExited || len(integrationStdoutBytes(t, screen)) == 0 {
			t.Fatalf("exited screen = %#v %q", screen.Result, integrationStdoutBytes(t, screen))
		}
		entry := integrationTerminalEntry(t, dispatchIntegrationOK(t, dispatcher, "terminal.list", map[string]any{}), handle)
		if entry["terminal_status"] != TerminalStatusExited {
			t.Fatalf("entry status = %#v", entry)
		}
		if code, ok := resultInt64(entry["exit_code"]); !ok || code != 0 {
			t.Fatalf("exit_code = %#v", entry["exit_code"])
		}
		closeIntegrationTerminal(t, dispatcher, handle)
	})

	t.Run("shell command mode", func(t *testing.T) {
		handle := openIntegrationTerminal(t, dispatcher, map[string]any{
			"name":    "shell-command",
			"command": "printf 'shell-mode\\n'; exit 7",
		})
		waitIntegrationStatus(t, dispatcher, handle, TerminalStatusExited)
		output := integrationReadAllOutput(t, dispatcher, handle)
		if !bytes.Contains(output, []byte("shell-mode")) {
			t.Fatalf("shell output = %q", output)
		}
		entry := integrationTerminalEntry(t, dispatchIntegrationOK(t, dispatcher, "terminal.list", map[string]any{}), handle)
		if code, ok := resultInt64(entry["exit_code"]); !ok || code != 7 {
			t.Fatalf("exit_code = %#v", entry["exit_code"])
		}
		closeIntegrationTerminal(t, dispatcher, handle)
	})

	t.Run("exact text write resize and foreground interrupt", func(t *testing.T) {
		handle := openIntegrationTerminal(t, dispatcher, map[string]any{
			"name":   "interactive",
			"argv":   []any{"/bin/bash", "--noprofile", "--norc", "-i"},
			"width":  100,
			"height": 35,
		})
		command := "printf '%s\\n' 'spaces \"quotes\" $dollar \\backslash'\n"
		dispatchIntegrationOK(t, dispatcher, "terminal.write", map[string]any{"handle": handle, "data": command})
		waitIntegrationOutput(t, dispatcher, handle, []byte("spaces \"quotes\" $dollar \\backslash"))

		resized := dispatchIntegrationOK(t, dispatcher, "terminal.resize", map[string]any{"handle": handle, "width": 132, "height": 43})
		if width, _ := resultInt64(resized.Result["width"]); width != 132 {
			t.Fatalf("resized width = %#v", resized.Result["width"])
		}
		if height, _ := resultInt64(resized.Result["height"]); height != 43 {
			t.Fatalf("resized height = %#v", resized.Result["height"])
		}
		dispatchIntegrationOK(t, dispatcher, "terminal.write", map[string]any{"handle": handle, "data": "stty size; printf '__SIZE_DONE__\\n'\n"})
		output := waitIntegrationOutput(t, dispatcher, handle, []byte("__SIZE_DONE__"))
		if !bytes.Contains(output, []byte("43 132")) {
			t.Fatalf("stty output = %q", output)
		}

		dispatchIntegrationOK(t, dispatcher, "terminal.write", map[string]any{"handle": handle, "data": "sleep 300\n"})
		waitIntegrationCurrentCommand(t, dispatcher, handle, "sleep")
		signaled := dispatchIntegrationOK(t, dispatcher, "terminal.signal", map[string]any{"handle": handle, "signal": "SIGINT"})
		if signaled.Result["signal"] != "SIGINT" {
			t.Fatalf("signal result = %#v", signaled.Result)
		}
		waitIntegrationCurrentCommand(t, dispatcher, handle, "bash")
		dispatchIntegrationOK(t, dispatcher, "terminal.write", map[string]any{"handle": handle, "data": "printf '__SHELL_ALIVE__\\n'\n"})
		waitIntegrationOutput(t, dispatcher, handle, []byte("__SHELL_ALIVE__"))
		closeIntegrationTerminal(t, dispatcher, handle)
	})

	t.Run("binary write", func(t *testing.T) {
		program := `import os,tty
tty.setraw(0)
os.write(1,b'READY\n')
data=b''
while len(data)<4:
    data+=os.read(0,4-len(data))
os.write(1,b'HEX:'+data.hex().encode()+b'\n')
`
		handle := openIntegrationTerminal(t, dispatcher, map[string]any{
			"name": "binary-write",
			"argv": []any{"/usr/bin/python3", "-c", program},
		})
		waitIntegrationOutput(t, dispatcher, handle, []byte("READY"))
		payload := []byte{0x00, 0xff, 0x0a, 0x41}
		dispatchIntegrationOK(t, dispatcher, "terminal.write", map[string]any{
			"handle": handle, "data_base64": base64.StdEncoding.EncodeToString(payload),
		})
		output := waitIntegrationOutput(t, dispatcher, handle, []byte("HEX:00ff0a41"))
		if !bytes.Contains(output, []byte("HEX:00ff0a41")) {
			t.Fatalf("binary output = %q", output)
		}
		waitIntegrationStatus(t, dispatcher, handle, TerminalStatusExited)
		closeIntegrationTerminal(t, dispatcher, handle)
	})

	t.Run("direct process signal remains readable", func(t *testing.T) {
		handle := openIntegrationTerminal(t, dispatcher, map[string]any{
			"name": "direct-signal",
			"argv": []any{"/bin/sleep", "300"},
		})
		dispatchIntegrationOK(t, dispatcher, "terminal.signal", map[string]any{"handle": handle, "signal": "SIGTERM"})
		waitIntegrationStatus(t, dispatcher, handle, TerminalStatusExited)
		var entry map[string]any
		waitIntegrationCondition(t, 3*time.Second, func() bool {
			entry = integrationTerminalEntryNoFail(dispatcher.Dispatch(context.Background(), CallRequest{Operation: "terminal.list", Args: map[string]any{}}), handle)
			return entry != nil && entry["signal"] == "SIGTERM"
		}, fmt.Sprintf("signal entry did not report SIGTERM: %#v", entry))
		dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "screen"})
		dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "output"})
		closeIntegrationTerminal(t, dispatcher, handle)
	})

	t.Run("multiple independent sessions", func(t *testing.T) {
		first := openIntegrationTerminal(t, dispatcher, map[string]any{"name": "multi-one", "argv": []any{"/bin/bash", "--noprofile", "--norc", "-i"}})
		second := openIntegrationTerminal(t, dispatcher, map[string]any{"name": "multi-two", "argv": []any{"/bin/bash", "--noprofile", "--norc", "-i"}})
		dispatchIntegrationOK(t, dispatcher, "terminal.write", map[string]any{"handle": first, "data": "printf '__FIRST_ONLY__\\n'\n"})
		firstOutput := waitIntegrationOutput(t, dispatcher, first, []byte("__FIRST_ONLY__"))
		secondOutput := integrationReadAllOutput(t, dispatcher, second)
		if !bytes.Contains(firstOutput, []byte("__FIRST_ONLY__")) || bytes.Contains(secondOutput, []byte("__FIRST_ONLY__")) {
			t.Fatalf("session isolation failed: first=%q second=%q", firstOutput, secondOutput)
		}
		dispatchIntegrationOK(t, dispatcher, "terminal.resize", map[string]any{"handle": first, "width": 111, "height": 31})
		secondEntry := integrationTerminalEntry(t, dispatchIntegrationOK(t, dispatcher, "terminal.list", map[string]any{}), second)
		if width, _ := resultInt64(secondEntry["width"]); width == 111 {
			t.Fatalf("resize affected second terminal: %#v", secondEntry)
		}
		closeIntegrationTerminal(t, dispatcher, first)
		entry := integrationTerminalEntry(t, dispatchIntegrationOK(t, dispatcher, "terminal.list", map[string]any{}), second)
		if entry["terminal_status"] != TerminalStatusRunning {
			t.Fatalf("closing first affected second: %#v", entry)
		}
		closeIntegrationTerminal(t, dispatcher, second)
	})

	t.Run("lost retained state", func(t *testing.T) {
		id, err := newTerminalID()
		if err != nil {
			t.Fatal(err)
		}
		directory := terminalDirectory(dispatcher.terminalsDir, id)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		metadata := TerminalMetadata{
			ID: id, Handle: terminalHandle(id), Name: "lost-state", Session: terminalSessionName(id),
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Width: 80, Height: 24,
		}
		if err := writeJSONAtomic(terminalMetadataPath(dispatcher.terminalsDir, id), metadata, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(terminalOutputPath(dispatcher.terminalsDir, id), []byte("retained-lost-output"), 0600); err != nil {
			t.Fatal(err)
		}
		handle := metadata.Handle
		entry := integrationTerminalEntry(t, dispatchIntegrationOK(t, dispatcher, "terminal.list", map[string]any{}), handle)
		if entry["terminal_status"] != TerminalStatusLost {
			t.Fatalf("lost entry = %#v", entry)
		}
		read := dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "output"})
		if !bytes.Equal(integrationStdoutBytes(t, read), []byte("retained-lost-output")) {
			t.Fatalf("lost output = %q", integrationStdoutBytes(t, read))
		}
		for operation, args := range map[string]map[string]any{
			"terminal.read":   {"handle": handle, "mode": "screen"},
			"terminal.write":  {"handle": handle, "data": "x"},
			"terminal.resize": {"handle": handle, "width": 80, "height": 24},
			"terminal.signal": {"handle": handle, "signal": "SIGINT"},
		} {
			failed := dispatcher.Dispatch(ctx, CallRequest{Operation: operation, Args: args})
			if failed.OK || failed.Error == nil || failed.Error.Code != "terminal_unavailable" {
				t.Fatalf("%s lost result = %#v", operation, failed)
			}
		}
		closed := dispatchIntegrationOK(t, dispatcher, "terminal.close", map[string]any{"handle": handle})
		if closed.Result["had_session"] != false {
			t.Fatalf("lost close = %#v", closed.Result)
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("lost directory remains: %v", err)
		}
	})

	entries, err := os.ReadDir(dispatcher.terminalsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("test terminal state remains: %#v", entries)
	}
	waitIntegrationCondition(t, 3*time.Second, func() bool {
		return !dispatcher.tmuxServerAlive(context.Background())
	}, "dedicated tmux server did not exit after its final session")
}

func TestTerminalSystemdScopeIntegration(t *testing.T) {
	if os.Getenv("HEC_TERMINAL_SYSTEMD_TEST") != "1" {
		t.Skip("set HEC_TERMINAL_SYSTEMD_TEST=1 for the host systemd scope test")
	}
	dispatcher := newTerminalIntegrationDispatcher(t, true)
	defer cleanupTerminalIntegrationDispatcher(t, dispatcher)
	handle := openIntegrationTerminal(t, dispatcher, map[string]any{"argv": []any{"/bin/sleep", "30"}})
	output, err := dispatcher.tmux(context.Background(), "display-message", "-p", "#{pid}")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatal(err)
	}
	cgroup, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cgroup, []byte(dispatcher.tmuxScopeName())) || bytes.Contains(cgroup, []byte("hec.service")) {
		t.Fatalf("tmux server cgroup = %q", cgroup)
	}
	closeIntegrationTerminal(t, dispatcher, handle)
	waitIntegrationCondition(t, 3*time.Second, func() bool {
		state, _ := dispatcher.tmuxScopeState(context.Background())
		return state == "inactive" || state == "absent"
	}, "tmux scope was not collected")
}

func newTerminalIntegrationDispatcher(t *testing.T, realScope bool) *Dispatcher {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "terminals")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "hec-terminal-helper")
	helperScript := "#!/bin/sh\nGO_WANT_TERMINAL_HELPER=1 exec " + shellSingleQuote(testBinary) + " -test.run=^TestTerminalHelperProcess$ -- \"$@\"\n"
	if err := os.WriteFile(helper, []byte(helperScript), 0700); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDispatcher()
	dispatcher.terminalsDir = state
	dispatcher.tmuxSocket = filepath.Join(root, "runtime", "tmux.sock")
	dispatcher.tmuxScopeUnit = "hec-tmux-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dispatcher.hecBinaryPath = helper
	if !realScope {
		fake := filepath.Join(root, "systemd-run")
		fakeScript := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != -- ]; do shift; done\n[ \"$#\" -gt 0 ] && shift\nexec \"$@\"\n"
		if err := os.WriteFile(fake, []byte(fakeScript), 0700); err != nil {
			t.Fatal(err)
		}
		dispatcher.systemdRunPath = fake
	}
	return dispatcher
}

func cleanupTerminalIntegrationDispatcher(t *testing.T, dispatcher *Dispatcher) {
	t.Helper()
	command := exec.Command(dispatcher.tmuxPath, "-S", dispatcher.tmuxSocket, "kill-server")
	_ = command.Run()
	if state, _ := dispatcher.tmuxScopeState(context.Background()); state == "active" || state == "activating" {
		_ = exec.Command(dispatcher.systemctlPath, "stop", dispatcher.tmuxScopeName()).Run()
	}
}

func dispatchIntegrationOK(t *testing.T, dispatcher *Dispatcher, operation string, args map[string]any) Result {
	t.Helper()
	result := dispatcher.Dispatch(context.Background(), CallRequest{Operation: operation, Args: args})
	if !result.OK {
		if result.Error != nil {
			t.Fatalf("%s failed: %s: %s", operation, result.Error.Code, result.Error.Message)
		}
		t.Fatalf("%s failed: %#v", operation, result)
	}
	return result
}

func openIntegrationTerminal(t *testing.T, dispatcher *Dispatcher, args map[string]any) string {
	t.Helper()
	result := dispatchIntegrationOK(t, dispatcher, "terminal.open", args)
	if result.Handle == nil || !strings.HasPrefix(*result.Handle, TerminalHandlePrefix) {
		t.Fatalf("terminal.open handle = %#v", result.Handle)
	}
	handle := *result.Handle
	t.Cleanup(func() {
		_ = dispatcher.Dispatch(context.Background(), CallRequest{Operation: "terminal.close", Args: map[string]any{"handle": handle}})
	})
	return handle
}

func closeIntegrationTerminal(t *testing.T, dispatcher *Dispatcher, handle string) {
	t.Helper()
	dispatchIntegrationOK(t, dispatcher, "terminal.close", map[string]any{"handle": handle})
}

func integrationTerminalEntry(t *testing.T, result Result, handle string) map[string]any {
	t.Helper()
	values, ok := result.Result["terminals"].([]any)
	if !ok {
		t.Fatalf("terminals = %#v", result.Result["terminals"])
	}
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if ok && entry["handle"] == handle {
			return entry
		}
	}
	t.Fatalf("terminal %s not listed: %#v", handle, values)
	return nil
}

func waitIntegrationStatus(t *testing.T, dispatcher *Dispatcher, handle, status string) {
	t.Helper()
	waitIntegrationCondition(t, 5*time.Second, func() bool {
		result := dispatcher.Dispatch(context.Background(), CallRequest{Operation: "terminal.list", Args: map[string]any{}})
		if !result.OK {
			return false
		}
		entry := integrationTerminalEntryNoFail(result, handle)
		return entry != nil && entry["terminal_status"] == status
	}, fmt.Sprintf("terminal %s did not reach %s", handle, status))
}

func waitIntegrationCurrentCommand(t *testing.T, dispatcher *Dispatcher, handle, command string) {
	t.Helper()
	var last map[string]any
	waitIntegrationCondition(t, 5*time.Second, func() bool {
		result := dispatcher.Dispatch(context.Background(), CallRequest{Operation: "terminal.list", Args: map[string]any{}})
		last = integrationTerminalEntryNoFail(result, handle)
		return last != nil && last["current_command"] == command
	}, fmt.Sprintf("terminal %s current command did not become %s; last=%#v", handle, command, last))
}

func integrationTerminalEntryNoFail(result Result, handle string) map[string]any {
	if !result.OK {
		return nil
	}
	values, ok := result.Result["terminals"].([]any)
	if !ok {
		return nil
	}
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if ok && entry["handle"] == handle {
			return entry
		}
	}
	return nil
}

func waitIntegrationOutput(t *testing.T, dispatcher *Dispatcher, handle string, needle []byte) []byte {
	t.Helper()
	var output []byte
	waitIntegrationCondition(t, 5*time.Second, func() bool {
		output = integrationReadAllOutput(t, dispatcher, handle)
		return bytes.Contains(output, needle)
	}, fmt.Sprintf("terminal %s output did not contain %q; got %q", handle, needle, output))
	return output
}

func integrationReadAllOutput(t *testing.T, dispatcher *Dispatcher, handle string) []byte {
	t.Helper()
	result := dispatchIntegrationOK(t, dispatcher, "terminal.read", map[string]any{"handle": handle, "mode": "output", "offset": 0, "limit": 1048576})
	return integrationStdoutBytes(t, result)
}

func integrationStdoutBytes(t *testing.T, result Result) []byte {
	t.Helper()
	switch result.StdoutEncoding {
	case "utf8":
		return []byte(result.Stdout)
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(result.Stdout)
		if err != nil {
			t.Fatal(err)
		}
		return decoded
	default:
		t.Fatalf("unexpected stdout encoding %q", result.StdoutEncoding)
		return nil
	}
}

func waitIntegrationCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal(message)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
