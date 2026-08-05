package hec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSystemdBackedJobsIntegration(t *testing.T) {
	if os.Getenv("HEC_SYSTEMD_TEST") != "1" {
		t.Skip("set HEC_SYSTEMD_TEST=1 to run systemd-backed job integration tests")
	}
	if os.Geteuid() != 0 {
		t.Skip("systemd-backed job integration test requires root")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("systemd is not available")
	}
	binary := os.Getenv("HEC_SYSTEMD_TEST_BINARY")
	if binary == "" {
		binary = JobBinaryPath
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("integration binary %q is unavailable: %v", binary, err)
	}

	root := t.TempDir()
	dispatcher := &Dispatcher{
		jobsDir:        filepath.Join(root, "jobs"),
		jobKeysDir:     filepath.Join(root, "job-keys"),
		systemdRunPath: "/usr/bin/systemd-run",
		systemctlPath:  "/usr/bin/systemctl",
		hecBinaryPath:  binary,
	}
	ctx := context.Background()
	var handles []string
	t.Cleanup(func() {
		for _, handle := range handles {
			id, err := parseJobHandle(handle)
			if err != nil {
				continue
			}
			unit := jobUnit(id)
			_ = runCleanupCommand("/usr/bin/systemctl", "stop", unit)
			_ = runCleanupCommand("/usr/bin/systemctl", "reset-failed", unit)
		}
	})

	key := fmt.Sprintf("hec-systemd-integration-%d", time.Now().UnixNano())
	start := dispatcher.Dispatch(ctx, CallRequest{
		Operation:      "job.start",
		IdempotencyKey: key,
		Args: map[string]any{
			"command": "printf 'one\\n'; sleep 1; printf 'two\\n'",
		},
	})
	if !start.OK || start.Handle == nil {
		t.Fatalf("job.start = %#v", start)
	}
	handle := *start.Handle
	handles = append(handles, handle)

	status := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.status",
		Args:      map[string]any{"handle": handle},
	})
	if !status.OK {
		t.Fatalf("job.status = %#v", status)
	}

	wait := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.wait",
		Args:      map[string]any{"handle": handle, "timeout_ms": 10000},
	})
	if !wait.OK || wait.Result["job_status"] != JobStatusCompleted || wait.Result["exit_code"] != float64(0) && wait.Result["exit_code"] != 0 {
		t.Fatalf("job.wait = %#v", wait)
	}

	output := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.output",
		Args:      map[string]any{"handle": handle, "stream": "stdout", "offset": 0, "limit": 4},
	})
	if !output.OK || output.Stdout != "one\n" {
		t.Fatalf("first job.output = %#v", output)
	}
	nextOffset, ok := resultInt64(output.Result["next_offset"])
	if !ok {
		t.Fatalf("next_offset = %#v", output.Result["next_offset"])
	}
	output = dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.output",
		Args:      map[string]any{"handle": handle, "stream": "stdout", "offset": nextOffset, "limit": 16},
	})
	if !output.OK || output.Stdout != "two\n" {
		t.Fatalf("second job.output = %#v", output)
	}

	repeated := dispatcher.Dispatch(ctx, CallRequest{
		Operation:      "job.start",
		IdempotencyKey: key,
		Args: map[string]any{
			"command": "printf duplicate",
		},
	})
	if !repeated.OK || repeated.Handle == nil || *repeated.Handle != handle {
		t.Fatalf("idempotent job.start = %#v", repeated)
	}

	sleepStart := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.start",
		Args: map[string]any{
			"argv": []any{"/bin/sleep", "30"},
		},
	})
	if !sleepStart.OK || sleepStart.Handle == nil {
		t.Fatalf("sleep job.start = %#v", sleepStart)
	}
	sleepHandle := *sleepStart.Handle
	handles = append(handles, sleepHandle)

	forgetRunning := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.forget",
		Args:      map[string]any{"handle": sleepHandle},
	})
	if forgetRunning.OK || forgetRunning.Error == nil || forgetRunning.Error.Code != "job_not_finished" {
		t.Fatalf("forget running job = %#v", forgetRunning)
	}

	waitTimeout := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.wait",
		Args:      map[string]any{"handle": sleepHandle, "timeout_ms": 50},
	})
	if !waitTimeout.OK || waitTimeout.Result["wait_timed_out"] != true {
		t.Fatalf("timed wait = %#v", waitTimeout)
	}

	signaled := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.signal",
		Args:      map[string]any{"handle": sleepHandle, "signal": "SIGTERM"},
	})
	if !signaled.OK {
		t.Fatalf("job.signal = %#v", signaled)
	}
	wait = dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.wait",
		Args:      map[string]any{"handle": sleepHandle, "timeout_ms": 10000},
	})
	if !wait.OK || wait.Result["job_status"] != JobStatusCancelled || wait.Result["signal"] != "SIGTERM" {
		t.Fatalf("signaled job.wait = %#v", wait)
	}

	listed := dispatcher.Dispatch(ctx, CallRequest{Operation: "job.list", Args: map[string]any{}})
	if !listed.OK {
		t.Fatalf("job.list = %#v", listed)
	}
	if !listedJobHandle(listed, handle) || !listedJobHandle(listed, sleepHandle) {
		t.Fatalf("job.list did not contain both jobs: %#v", listed)
	}

	forgotten := dispatcher.Dispatch(ctx, CallRequest{
		Operation: "job.forget",
		Args:      map[string]any{"handle": handle},
	})
	if !forgotten.OK {
		t.Fatalf("job.forget = %#v", forgotten)
	}
	if _, err := os.Stat(filepath.Join(dispatcher.jobsDir, mustJobID(t, handle))); !os.IsNotExist(err) {
		t.Fatalf("forgotten job directory still exists: %v", err)
	}
}

func listedJobHandle(result Result, handle string) bool {
	jobs, ok := result.Result["jobs"].([]any)
	if !ok {
		return false
	}
	for _, raw := range jobs {
		entry, ok := raw.(map[string]any)
		if ok && entry["handle"] == handle {
			return true
		}
	}
	return false
}

func mustJobID(t *testing.T, handle string) string {
	t.Helper()
	id, err := parseJobHandle(handle)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func runCleanupCommand(name string, args ...string) error {
	process, err := os.StartProcess(name, append([]string{name}, args...), &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return err
	}
	_, err = process.Wait()
	return err
}
