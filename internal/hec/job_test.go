package hec

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobHandleGenerationAndParsing(t *testing.T) {
	raw := make([]byte, JobIDRandomBytes)
	for index := range raw {
		raw[index] = byte(index)
	}
	id, err := generateJobID(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("generate job id: %v", err)
	}
	if len(id) != 22 {
		t.Fatalf("id length = %d, want 22", len(id))
	}
	handle := jobHandle(id)
	parsed, err := parseJobHandle(handle)
	if err != nil {
		t.Fatalf("parse generated handle: %v", err)
	}
	if parsed != id {
		t.Fatalf("parsed id = %q, want %q", parsed, id)
	}

	seen := make(map[string]bool)
	for range 100 {
		generated, err := newJobID()
		if err != nil {
			t.Fatalf("new job id: %v", err)
		}
		if seen[generated] {
			t.Fatalf("duplicate generated id %q", generated)
		}
		seen[generated] = true
		if _, err := parseJobHandle(jobHandle(generated)); err != nil {
			t.Fatalf("generated id was not parseable: %v", err)
		}
	}
}

func TestInvalidJobHandles(t *testing.T) {
	for _, handle := range []string{
		"",
		"job:",
		"task:AAAAAAAAAAAAAAAAAAAAAA",
		"job:short",
		"job:AAAAAAAAAAAAAAAAAAAAA!",
		"job:AAAAAAAAAAAAAAAAAAAAAAA",
	} {
		if _, err := parseJobHandle(handle); err == nil {
			t.Errorf("parseJobHandle(%q) succeeded", handle)
		}
	}
}

func TestJobSpecificationDecoding(t *testing.T) {
	directory := t.TempDir()
	validPath := filepath.Join(directory, "valid.json")
	if err := os.WriteFile(validPath, []byte(`{"argv":["/usr/bin/printf","ok"],"cwd":"","env":{"A":"B"},"unset_env":["OLD"],"timeout_ms":10}`), 0600); err != nil {
		t.Fatal(err)
	}
	spec, err := decodeJobSpec(validPath)
	if err != nil {
		t.Fatalf("decode valid spec: %v", err)
	}
	if spec.CWD != DefaultCWD || len(spec.Argv) != 2 || spec.Argv[0] != "/usr/bin/printf" {
		t.Fatalf("decoded spec = %#v", spec)
	}

	cases := map[string]string{
		"unknown field":    `{"argv":["/bin/true"],"cwd":"/root","unexpected":true}`,
		"both forms":       `{"command":"true","argv":["/bin/true"],"cwd":"/root"}`,
		"neither form":     `{"cwd":"/root"}`,
		"empty executable": `{"argv":[""],"cwd":"/root"}`,
		"negative timeout": `{"argv":["/bin/true"],"cwd":"/root","timeout_ms":-1}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, strings.ReplaceAll(name, " ", "_")+".json")
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := decodeJobSpec(path); err == nil {
				t.Fatal("invalid job spec was accepted")
			}
		})
	}
}

func TestJobResultFileParsing(t *testing.T) {
	directory := t.TempDir()
	exitCode := 7
	valid := jobFinalResult{
		Status:   JobStatusFailed,
		ExitCode: &exitCode,
		TimedOut: false,
		Complete: true,
	}
	path := filepath.Join(directory, "result.json")
	if err := writeJSONAtomic(path, valid, 0600); err != nil {
		t.Fatal(err)
	}
	decoded, err := readJobFinalResult(path)
	if err != nil {
		t.Fatalf("read valid result: %v", err)
	}
	if decoded.Status != JobStatusFailed || decoded.ExitCode == nil || *decoded.ExitCode != 7 {
		t.Fatalf("decoded result = %#v", decoded)
	}

	if err := os.WriteFile(path, []byte(`{"status":"completed","exit_code":0,"signal":null,"timed_out":false,"complete":true,"extra":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readJobFinalResult(path); err == nil {
		t.Fatal("result with unknown field was accepted")
	}

	if err := os.WriteFile(path, []byte(`{"status":"mystery","exit_code":0,"signal":null,"timed_out":false,"complete":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readJobFinalResult(path); err == nil {
		t.Fatal("result with invalid status was accepted")
	}
}

func TestJobOutputRangesUTF8AndBase64(t *testing.T) {
	directory := t.TempDir()
	utf8Path := filepath.Join(directory, "utf8")
	if err := os.WriteFile(utf8Path, []byte("alpha-beta"), 0600); err != nil {
		t.Fatal(err)
	}
	chunk, total, next, eof, err := readOutputRange(utf8Path, 6, 4)
	if err != nil {
		t.Fatal(err)
	}
	value, encoding := encodeOutput(chunk)
	if value != "beta" || encoding != "utf8" || total != 10 || next != 10 || !eof {
		t.Fatalf("range = value %q encoding %q total %d next %d eof %v", value, encoding, total, next, eof)
	}

	binaryPath := filepath.Join(directory, "binary")
	binary := []byte{0x00, 0xff, 0x01, 0x02}
	if err := os.WriteFile(binaryPath, binary, 0600); err != nil {
		t.Fatal(err)
	}
	chunk, total, next, eof, err = readOutputRange(binaryPath, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	value, encoding = encodeOutput(chunk)
	if encoding != "base64" || value != base64.StdEncoding.EncodeToString(binary[1:3]) || total != 4 || next != 3 || eof {
		t.Fatalf("binary range = value %q encoding %q total %d next %d eof %v", value, encoding, total, next, eof)
	}
}

func TestIdempotencyKeyHashAndMapping(t *testing.T) {
	if got := jobKeyDigest("slice2-key"); got != "91bd696333198e04398c5b299471ccf377c959c21c3d3e4f2f981017d9c7735c" {
		t.Fatalf("digest = %q", got)
	}
	directory := t.TempDir()
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, jobKeyDigest("slice2-key"))
	if err := writeTextAtomic(path, id+"\n", 0600); err != nil {
		t.Fatal(err)
	}
	mapped, exists, err := readJobKeyMapping(path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || mapped != id {
		t.Fatalf("mapping = %q, %v", mapped, exists)
	}
}

func TestUnknownHandleReturnsNotFound(t *testing.T) {
	dispatcher := testJobDispatcher(t)
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	result := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "job.status",
		Args:      map[string]any{"handle": jobHandle(id)},
	})
	if result.OK || result.Error == nil || result.Error.Code != "job_not_found" {
		t.Fatalf("result = %#v", result)
	}
}

func TestJobOutputArgumentDefaultsAndBounds(t *testing.T) {
	dispatcher := testJobDispatcher(t)
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	jobDirectory := filepath.Join(dispatcher.jobsDir, id)
	if err := os.MkdirAll(jobDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDirectory, "stdout"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDirectory, "stderr"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	handle := jobHandle(id)
	result := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "job.output",
		Args:      map[string]any{"handle": handle, "stream": "stdout"},
	})
	if !result.OK || result.Stdout != "hello" {
		t.Fatalf("default output result = %#v", result)
	}
	result = dispatcher.Dispatch(context.Background(), CallRequest{
		Operation: "job.output",
		Args:      map[string]any{"handle": handle, "stream": "stdout", "limit": 0},
	})
	if result.OK || result.Error == nil || result.Error.Code != "invalid_arguments" {
		t.Fatalf("zero limit result = %#v", result)
	}
}

func TestJobStartArgumentValidation(t *testing.T) {
	dispatcher := testJobDispatcher(t)
	cases := []map[string]any{
		{},
		{"command": "true", "argv": []any{"/bin/true"}},
		{"argv": []any{}},
		{"command": "true", "stdin": "a", "stdin_base64": "Yg=="},
		{"command": "true", "timeout_ms": -1},
		{"command": "true", "unexpected": true},
	}
	for _, args := range cases {
		result := dispatcher.Dispatch(context.Background(), CallRequest{Operation: "job.start", Args: args})
		if result.OK || result.Error == nil || result.Error.Code != "invalid_arguments" {
			t.Fatalf("args %#v result = %#v", args, result)
		}
	}
}

func testJobDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	root := t.TempDir()
	systemctl := filepath.Join(root, "systemctl")
	script := `#!/bin/sh
case "$1" in
  show)
    printf '%s\n' 'LoadState=not-found' 'ActiveState=inactive' 'SubState=dead' 'Result=' 'ExecMainCode=0' 'ExecMainStatus=0'
    ;;
  list-units)
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(systemctl, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return &Dispatcher{
		jobsDir:        filepath.Join(root, "jobs"),
		jobKeysDir:     filepath.Join(root, "keys"),
		systemdRunPath: "/bin/false",
		systemctlPath:  systemctl,
		hecBinaryPath:  "/bin/false",
	}
}

func TestJobResultSummaries(t *testing.T) {
	handle := "job:AAAAAAAAAAAAAAAAAAAAAA"
	tests := []struct {
		name   string
		result Result
		want   string
	}{
		{
			name: "start",
			result: Result{
				OK:        true,
				Operation: "job.start",
				Handle:    &handle,
			},
			want: "Started job:AAAAAAAAAAAAAAAAAAAAAA.",
		},
		{
			name: "status",
			result: Result{
				OK:        true,
				Operation: "job.status",
				Result: map[string]any{
					"handle":     handle,
					"job_status": JobStatusRunning,
				},
			},
			want: "job:AAAAAAAAAAAAAAAAAAAAAA is running.",
		},
		{
			name: "output",
			result: Result{
				OK:        true,
				Operation: "job.output",
				Handle:    &handle,
				Result: map[string]any{
					"stream":      "stdout",
					"offset":      int64(10),
					"next_offset": int64(25),
				},
			},
			want: "Read 15 bytes from job:AAAAAAAAAAAAAAAAAAAAAA stdout.",
		},
		{
			name: "signal",
			result: Result{
				OK:        true,
				Operation: "job.signal",
				Handle:    &handle,
				Result:    map[string]any{"signal": "SIGTERM"},
			},
			want: "Sent SIGTERM to job:AAAAAAAAAAAAAAAAAAAAAA.",
		},
		{
			name: "list",
			result: Result{
				OK:        true,
				Operation: "job.list",
				Result:    map[string]any{"count": 2},
			},
			want: "Listed 2 jobs.",
		},
		{
			name: "forget",
			result: Result{
				OK:        true,
				Operation: "job.forget",
				Handle:    &handle,
			},
			want: "Forgot job:AAAAAAAAAAAAAAAAAAAAAA.",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.result.Summary(); got != testCase.want {
				t.Fatalf("summary = %q, want %q", got, testCase.want)
			}
		})
	}
}
