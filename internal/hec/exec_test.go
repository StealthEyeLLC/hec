package hec

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestDispatcherHealthAndVersion(t *testing.T) {
	dispatcher := NewDispatcher()

	health := dispatcher.Dispatch(context.Background(), CallRequest{Operation: "health"})
	if !health.OK || health.Result["alive"] != true {
		t.Fatalf("health result = %#v", health)
	}

	version := dispatcher.Dispatch(context.Background(), CallRequest{Operation: "version"})
	if !version.OK || version.Result["version"] != Version || version.Result["protocol"] != ProtocolVersion {
		t.Fatalf("version result = %#v", version)
	}
}

func TestRunCommandAndArgv(t *testing.T) {
	dispatcher := NewDispatcher()

	shell := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"command": "printf shell",
		},
	})
	if !shell.OK || shell.Stdout != "shell" || shell.ExitCode == nil || *shell.ExitCode != 0 {
		t.Fatalf("shell result = %#v", shell)
	}

	direct := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv": []any{"/usr/bin/printf", "direct"},
		},
	})
	if !direct.OK || direct.Stdout != "direct" || direct.ExitCode == nil || *direct.ExitCode != 0 {
		t.Fatalf("direct result = %#v", direct)
	}
}

func TestRunCWDEnvironmentAndStdin(t *testing.T) {
	dispatcher := NewDispatcher()
	tempDir := t.TempDir()
	stdin := "input"
	result := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"command": "printf '%s|' \"$HEC_TEST_VALUE\"; pwd; cat",
			"cwd":     tempDir,
			"env": map[string]any{
				"HEC_TEST_VALUE": "environment",
			},
			"stdin": stdin,
		},
	})
	if !result.OK {
		t.Fatalf("run failed: %#v", result)
	}
	want := "environment|" + tempDir + "\n" + stdin
	if result.Stdout != want {
		t.Fatalf("stdout = %q, want %q", result.Stdout, want)
	}
}

func TestRunBinaryInputAndOutput(t *testing.T) {
	dispatcher := NewDispatcher()
	input := []byte{0x00, 0xff, 0x01}
	result := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv":         []any{"/bin/cat"},
			"stdin_base64": base64.StdEncoding.EncodeToString(input),
		},
	})
	if !result.OK {
		t.Fatalf("run failed: %#v", result)
	}
	if result.StdoutEncoding != "base64" {
		t.Fatalf("stdout encoding = %q", result.StdoutEncoding)
	}
	if result.Stdout != base64.StdEncoding.EncodeToString(input) {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRunTruncation(t *testing.T) {
	result := NewDispatcher().Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"command":          "printf abcdef",
			"max_output_bytes": 3,
		},
	})
	if !result.OK || !result.Truncated || result.Stdout != "abc" {
		t.Fatalf("truncated result = %#v", result)
	}
}

func TestRunTimeout(t *testing.T) {
	result := NewDispatcher().Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv":       []any{"/bin/sleep", "5"},
			"timeout_ms": 20,
		},
	})
	if result.OK || result.Status != StatusTimedOut {
		t.Fatalf("timeout result = %#v", result)
	}
	if result.Signal == nil || *result.Signal != "SIGKILL" {
		t.Fatalf("timeout signal = %#v", result.Signal)
	}
}

func TestRunNonzeroExit(t *testing.T) {
	result := NewDispatcher().Dispatch(context.Background(), CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv": []any{"/bin/sh", "-c", "printf error >&2; exit 7"},
		},
	})
	if result.OK || result.Status != StatusFailed || result.ExitCode == nil || *result.ExitCode != 7 {
		t.Fatalf("failure result = %#v", result)
	}
	if result.Stderr != "error" || result.Error == nil || result.Error.Code != "process_failed" {
		t.Fatalf("failure details = %#v", result)
	}
}

func TestRunArgumentValidation(t *testing.T) {
	tests := []map[string]any{
		{},
		{"command": "true", "argv": []any{"/bin/true"}},
		{"argv": []any{}},
		{"command": "true", "stdin": "a", "stdin_base64": "Yg=="},
		{"command": "true", "unknown": true},
	}
	for _, args := range tests {
		result := NewDispatcher().Dispatch(context.Background(), CallRequest{Operation: "run", Args: args})
		if result.OK || result.Error == nil || !strings.HasPrefix(result.Error.Code, "invalid_") {
			t.Fatalf("args %#v result = %#v", args, result)
		}
	}
}
