package hec

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFixedDeadlineConstants(t *testing.T) {
	checks := map[string]struct {
		got  time.Duration
		want time.Duration
	}{
		"MaxDirectCall":            {MaxDirectCall, 90 * time.Second},
		"MaxJobWait":               {MaxJobWait, 15 * time.Second},
		"ResponseDeliveryReserve":  {ResponseDeliveryReserve, 10 * time.Second},
		"CallGateAcquireTimeout":   {CallGateAcquireTimeout, 10 * time.Second},
		"GenerationReadyTimeout":   {GenerationReadyTimeout, 60 * time.Second},
		"TunnelStopTimeout":        {TunnelStopTimeout, 5 * time.Second},
		"GenerationCleanupTimeout": {GenerationCleanupTimeout, 10 * time.Second},
	}
	for name, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %s, want %s", name, check.got, check.want)
		}
	}
}

func TestDurationConversionRejectsOverflow(t *testing.T) {
	if _, err := durationFromMilliseconds(math.MaxInt64); !errors.Is(err, errInvalidDurationMilliseconds) {
		t.Fatalf("overflow error = %v", err)
	}
	maximumSafe := int64(math.MaxInt64 / int64(time.Millisecond))
	if _, err := durationFromMilliseconds(maximumSafe); err != nil {
		t.Fatalf("maximum safe duration: %v", err)
	}
}

func TestOmittedAndOversizedRunTimeouts(t *testing.T) {
	got, err := directRunTimeout(0)
	if err != nil || got != MaxDirectCall {
		t.Fatalf("omitted timeout = %s, %v", got, err)
	}
	if _, err := directRunTimeout(MaxDirectCall.Milliseconds() + 1); err == nil {
		t.Fatal("oversized run timeout accepted")
	}
}

func TestShorterCallerDeadlineWinsForRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := NewDispatcher().run(ctx, map[string]any{
		"argv": []any{"/bin/sh", "-c", "sleep 30"},
	})
	if result.Error == nil || result.Error.Code != "timed_out" {
		t.Fatalf("result = %#v", result)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("short caller deadline took %s", elapsed)
	}
}

func TestTimedOutRunTerminatesProcessGroup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	result := NewDispatcher().run(context.Background(), map[string]any{
		"argv":       []any{"/bin/sh", "-c", "sleep 30 & echo $! > \"$1\"; wait", "hec-test", pidPath},
		"timeout_ms": 100,
	})
	if result.Error == nil || result.Error.Code != "timed_out" {
		t.Fatalf("result = %#v", result)
	}
	payload, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(payload)))
	if err != nil {
		t.Fatal(err)
	}
	processPath := filepath.Join("/proc", strconv.Itoa(pid))
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(processPath); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived process-group timeout", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestJobWaitBudgetDefaultsAndCaps(t *testing.T) {
	got, err := jobWaitTimeout(0)
	if err != nil || got != MaxJobWait {
		t.Fatalf("omitted job wait = %s, %v", got, err)
	}
	if _, err := jobWaitTimeout(MaxJobWait.Milliseconds() + 1); err == nil {
		t.Fatal("oversized job wait accepted")
	}
}

func TestJobWaitReturnsRunningResultWithinBudget(t *testing.T) {
	dispatcher := NewDispatcher()
	dispatcher.jobsDir = t.TempDir()
	id, err := generateJobID(zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(dispatcher.jobsDir, id)
	if err := os.Mkdir(jobDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"stdout", "stderr"} {
		if err := os.WriteFile(filepath.Join(jobDir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	systemctl := filepath.Join(t.TempDir(), "systemctl")
	if err := os.WriteFile(systemctl, []byte("#!/bin/sh\nprintf 'LoadState=not-found\\nActiveState=inactive\\nSubState=dead\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	dispatcher.systemctlPath = systemctl
	started := time.Now()
	result := dispatcher.jobWait(context.Background(), map[string]any{
		"handle":     jobHandle(id),
		"timeout_ms": 25,
	})
	if !result.OK {
		t.Fatalf("job.wait = %#v", result)
	}
	if timedOut, _ := result.Result["wait_timed_out"].(bool); !timedOut {
		t.Fatalf("job.wait result = %#v", result.Result)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("job.wait exceeded bounded budget: %s", elapsed)
	}
}

func TestDirectCallReservesResponseDeliveryTime(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelParent()
	call, cancelCall := directCallContext(parent)
	defer cancelCall()
	deadline, ok := call.Deadline()
	if !ok {
		t.Fatal("direct call has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 19*time.Second || remaining > 21*time.Second {
		t.Fatalf("reserved direct-call budget = %s, want about 20s", remaining)
	}
}

func TestExpiredPublicCallDropsLateDispatchResult(t *testing.T) {
	handler := newPublicCallHandler(dispatchFunc(func(context.Context, CallRequest) Result {
		time.Sleep(40 * time.Millisecond)
		result := newResult("health")
		result.OK = true
		return result
	}))
	ctx, cancel := context.WithTimeout(context.Background(), ResponseDeliveryReserve+20*time.Millisecond)
	defer cancel()
	result := handler.dispatch(ctx, CallRequest{Operation: "health", Args: map[string]any{}})
	if result.OK || result.Error == nil || result.Error.Code != "timed_out" {
		t.Fatalf("late dispatch result was delivered: %#v", result)
	}
}

func TestTunnelClientIsPinnedToReviewedResponseTimeoutCommit(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "github.com/openai/tunnel-client v0.0.11-0.20260806014146-1bf01b0e1079"
	if !strings.Contains(string(payload), want) {
		t.Fatalf("go.mod does not pin %s", want)
	}
}

func bytesTrimSpace(value []byte) []byte {
	start := 0
	for start < len(value) && (value[start] == ' ' || value[start] == '\n' || value[start] == '\r' || value[start] == '\t') {
		start++
	}
	end := len(value)
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\r' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}

type zeroReader struct{}

func (zeroReader) Read(payload []byte) (int, error) {
	for index := range payload {
		payload[index] = 0
	}
	return len(payload), nil
}
