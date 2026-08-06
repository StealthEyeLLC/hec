package hec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newKeyedTestDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	dispatcher := NewDispatcher()
	root := t.TempDir()
	dispatcher.mutationKeysDir = filepath.Join(root, "mutation-keys")
	dispatcher.keyedState = newKeyedMutationStore(dispatcher.mutationKeysDir)
	return dispatcher
}

func TestKeyedMutationPublishesInProgressBeforeExecutionAndCompletedBeforeReturn(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	key := "durability-key"
	statePath := dispatcher.keyedState.statePath(digestString(key))
	marker := filepath.Join(t.TempDir(), "observed")
	request := CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv": []any{"/bin/sh", "-c", "grep -q '\"state\":\"in_progress\"' \"$1\" && printf yes > \"$2\"", "hec-test", statePath, marker},
		},
		IdempotencyKey: key,
	}
	result := dispatcher.Dispatch(context.Background(), request)
	if !result.OK {
		t.Fatalf("keyed run = %#v", result)
	}
	if payload, err := os.ReadFile(marker); err != nil || string(payload) != "yes" {
		t.Fatalf("native execution did not observe durable in_progress: %q, %v", payload, err)
	}
	record, found, err := dispatcher.keyedState.read(digestString(key))
	if err != nil || !found {
		t.Fatalf("read completed state: found=%v err=%v", found, err)
	}
	if record.State != keyedStateCompleted || record.Result == nil {
		t.Fatalf("record = %#v", record)
	}
}

func TestKeyedMutationCompletedReplayDoesNotReexecute(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	target := filepath.Join(t.TempDir(), "append.txt")
	request := CallRequest{
		Operation:      "file.append",
		Args:           map[string]any{"path": target, "content": "x"},
		IdempotencyKey: "same-request",
	}
	first := dispatcher.Dispatch(context.Background(), request)
	second := dispatcher.Dispatch(context.Background(), request)
	if !first.OK || !second.OK {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("replayed public JSON changed:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "x" {
		t.Fatalf("native mutation executed more than once: %q", payload)
	}
}

func TestKeyedMutationChangedRequestConflicts(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	target := filepath.Join(t.TempDir(), "append.txt")
	first := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation:      "file.append",
		Args:           map[string]any{"path": target, "content": "x"},
		IdempotencyKey: "conflict-key",
	})
	if !first.OK {
		t.Fatal(first)
	}
	second := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation:      "file.append",
		Args:           map[string]any{"path": target, "content": "y"},
		IdempotencyKey: "conflict-key",
	})
	if second.Error == nil || second.Error.Code != "idempotency_conflict" {
		t.Fatalf("changed request = %#v", second)
	}
	payload, err := os.ReadFile(target)
	if err != nil || string(payload) != "x" {
		t.Fatalf("target = %q, %v", payload, err)
	}
}

func TestKeyedMutationOrphanedInProgressIsUncertainAndDoesNotExecute(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	target := filepath.Join(t.TempDir(), "append.txt")
	request := CallRequest{
		Operation:      "file.append",
		Args:           map[string]any{"path": target, "content": "x"},
		IdempotencyKey: "orphan-key",
	}
	_, requestHash, err := normalizeMutationRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	digest := digestString(request.IdempotencyKey)
	if err := dispatcher.keyedState.write(digest, keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       keyedStateInProgress,
		Operation:   request.Operation,
		RequestHash: requestHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	result := dispatcher.Dispatch(context.Background(), request)
	if result.Error == nil || result.Error.Code != "uncertain_prior_execution" {
		t.Fatalf("orphan result = %#v", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphaned mutation executed: %v", err)
	}
}

func TestKeyedMutationCorruptStateFailsClosed(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	target := filepath.Join(t.TempDir(), "append.txt")
	key := "corrupt-key"
	if err := dispatcher.keyedState.ensureRoot(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dispatcher.keyedState.statePath(digestString(key)), []byte("{not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	result := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation:      "file.append",
		Args:           map[string]any{"path": target, "content": "x"},
		IdempotencyKey: key,
	})
	if result.Error == nil || result.Error.Code != "idempotency_state_corrupt" {
		t.Fatalf("corrupt state result = %#v", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt-state mutation executed: %v", err)
	}
}

func TestKeyedMutationConcurrentSameKeyExecutesAtMostOnce(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	target := filepath.Join(t.TempDir(), "count.txt")
	request := CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv": []any{"/bin/sh", "-c", "sleep 0.05; printf x >> \"$1\"", "hec-test", target},
		},
		IdempotencyKey: "concurrent-key",
	}
	const callers = 20
	results := make(chan Result, callers)
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			results <- dispatcher.Dispatch(context.Background(), request)
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result.OK {
			continue
		}
		if result.Error == nil || result.Error.Code != "idempotency_in_progress" {
			t.Fatalf("unexpected concurrent result = %#v", result)
		}
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "x" {
		t.Fatalf("same key executed multiple times: %q", payload)
	}
}

func TestKeyedMutationDifferentKeysDoNotShareGlobalExecutionLock(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	started := time.Now()
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan Result, 2)
	for _, key := range []string{"parallel-a", "parallel-b"} {
		key := key
		go func() {
			defer wait.Done()
			results <- dispatcher.Dispatch(context.Background(), CallRequest{
				Operation:      "run",
				Args:           map[string]any{"argv": []any{"/bin/sh", "-c", "sleep 0.3"}},
				IdempotencyKey: key,
			})
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if !result.OK {
			t.Fatalf("parallel result = %#v", result)
		}
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("different keys were globally serialized: %s", elapsed)
	}
}

func TestKeyedMutationUsesHashedPrivatePaths(t *testing.T) {
	dispatcher := newKeyedTestDispatcher(t)
	key := "raw/key/value"
	target := filepath.Join(t.TempDir(), "append.txt")
	result := dispatcher.Dispatch(context.Background(), CallRequest{
		Operation:      "file.append",
		Args:           map[string]any{"path": target, "content": "x"},
		IdempotencyKey: key,
	})
	if !result.OK {
		t.Fatal(result)
	}
	entries, err := os.ReadDir(dispatcher.mutationKeysDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "raw") || strings.Contains(entry.Name(), "key") || strings.Contains(entry.Name(), "value") {
			t.Fatalf("raw idempotency key leaked into path %q", entry.Name())
		}
	}
	info, err := os.Stat(dispatcher.mutationKeysDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("mutation key directory mode = %o", info.Mode().Perm())
	}
}

func TestDurableWritePublishesCompleteContentAndRemovesTemporaryFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.json")
	payload := []byte(`{"state":"completed"}`)
	if err := writeFileDurable(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	read, err := os.ReadFile(path)
	if err != nil || string(read) != string(payload) {
		t.Fatalf("read = %q, %v", read, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("directory entries = %#v", entries)
	}
}

func TestKeyedStateUnknownStateFailsValidation(t *testing.T) {
	record := keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       "unknown",
		Operation:   "file.append",
		RequestHash: strings.Repeat("0", 64),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"state":"unknown"`) {
		t.Fatal("test record did not encode unknown state")
	}
	if err := validateKeyedMutationRecord(record); err == nil {
		t.Fatal("unknown keyed state accepted")
	}
}

func TestKeyedRequestDefaultsAreSemantic(t *testing.T) {
	requestA := CallRequest{
		Operation:      "run",
		Args:           map[string]any{"argv": []any{"/bin/true"}},
		IdempotencyKey: "semantic",
	}
	requestB := CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv":             []any{"/bin/true"},
			"cwd":              DefaultCWD,
			"timeout_ms":       MaxDirectCall.Milliseconds(),
			"max_output_bytes": DefaultMaxOutputBytes,
		},
		IdempotencyKey: "semantic",
	}
	_, hashA, err := normalizeMutationRequest(requestA)
	if err != nil {
		t.Fatal(err)
	}
	_, hashB, err := normalizeMutationRequest(requestB)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatalf("semantic defaults changed hash: %s != %s", hashA, hashB)
	}
}

func Example_keyedMutationState() {
	fmt.Println(keyedStateInProgress, keyedStateCompleted)
	// Output: in_progress completed
}

func TestKeyedRequestCanonicalizesEquivalentByteAndEnvironmentForms(t *testing.T) {
	runText := CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv":      []any{"/bin/cat"},
			"stdin":     "hello",
			"env":       map[string]any{},
			"unset_env": []any{"B", "A"},
		},
	}
	runBase64 := CallRequest{
		Operation: "run",
		Args: map[string]any{
			"argv":         []any{"/bin/cat"},
			"stdin_base64": "aGVsbG8=",
			"unset_env":    []any{"A", "B"},
		},
	}
	_, firstHash, err := normalizeMutationRequest(runText)
	if err != nil {
		t.Fatal(err)
	}
	_, secondHash, err := normalizeMutationRequest(runBase64)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("equivalent run forms hash differently: %s != %s", firstHash, secondHash)
	}

	path := filepath.Join(t.TempDir(), "value")
	fileText := CallRequest{Operation: "file.write", Args: map[string]any{"path": path, "content": "hello"}}
	fileBase64 := CallRequest{Operation: "file.write", Args: map[string]any{"path": path, "content_base64": "aGVsbG8="}}
	_, firstHash, err = normalizeMutationRequest(fileText)
	if err != nil {
		t.Fatal(err)
	}
	_, secondHash, err = normalizeMutationRequest(fileBase64)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("equivalent file forms hash differently: %s != %s", firstHash, secondHash)
	}
}
