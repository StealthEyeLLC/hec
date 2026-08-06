package hec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type keyedJobFixture struct {
	dispatcher *Dispatcher
	root       string
	stateDir   string
	callsPath  string
}

func newKeyedJobFixture(t *testing.T, mode string) keyedJobFixture {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, "systemd-state")
	if err := os.Mkdir(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	callsPath := filepath.Join(root, "systemd-run.calls")
	runPath := filepath.Join(root, "systemd-run")
	controlPath := filepath.Join(root, "systemctl")

	runScript := `#!/bin/sh
set -eu
unit=""
for argument in "$@"; do
  case "$argument" in
    --unit=*) unit=${argument#--unit=} ;;
  esac
done
[ -n "$unit" ]
printf '%s\n' "$unit" >> ` + shellQuoteForTest(callsPath) + `
case ` + shellQuoteForTest(mode) + ` in
  success)
    : > ` + shellQuoteForTest(stateDir) + `/"$unit.service"
    exit 0
    ;;
  lost)
    : > ` + shellQuoteForTest(stateDir) + `/"$unit.service"
    exit 1
    ;;
  fail)
    exit 1
    ;;
  *)
    exit 2
    ;;
esac
`
	controlScript := `#!/bin/sh
set -eu
for unit do :; done
if [ -e ` + shellQuoteForTest(stateDir) + `/"$unit" ]; then
  printf '%s\n' \
    'LoadState=loaded' \
    'ActiveState=active' \
    'SubState=running' \
    'Result=success' \
    'ExecMainCode=' \
    'ExecMainStatus='
else
  printf '%s\n' \
    'LoadState=not-found' \
    'ActiveState=inactive' \
    'SubState=dead' \
    'Result=' \
    'ExecMainCode=' \
    'ExecMainStatus='
fi
`
	if err := os.WriteFile(runPath, []byte(runScript), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlPath, []byte(controlScript), 0700); err != nil {
		t.Fatal(err)
	}

	dispatcher := NewDispatcher()
	dispatcher.jobsDir = filepath.Join(root, "jobs")
	dispatcher.jobKeysDir = filepath.Join(root, "legacy-job-keys")
	dispatcher.mutationKeysDir = filepath.Join(root, "mutation-keys")
	dispatcher.keyedState = newKeyedMutationStore(dispatcher.mutationKeysDir)
	dispatcher.systemdRunPath = runPath
	dispatcher.systemctlPath = controlPath
	dispatcher.hecBinaryPath = "/bin/true"
	return keyedJobFixture{dispatcher: dispatcher, root: root, stateDir: stateDir, callsPath: callsPath}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func keyedJobRequest(key string, argv ...string) CallRequest {
	values := make([]any, len(argv))
	for index, value := range argv {
		values[index] = value
	}
	return CallRequest{
		Operation:      "job.start",
		Args:           map[string]any{"argv": values},
		IdempotencyKey: key,
	}
}

func TestKeyedJobStartSameRequestReturnsSameJobAndChangedRequestConflicts(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	request := keyedJobRequest("job-key", "/bin/true")
	first := fixture.dispatcher.Dispatch(context.Background(), request)
	second := fixture.dispatcher.Dispatch(context.Background(), request)
	if !first.OK || !second.OK || first.Handle == nil || second.Handle == nil {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if *first.Handle != *second.Handle {
		t.Fatalf("same key returned different jobs: %s != %s", *first.Handle, *second.Handle)
	}
	changed := fixture.dispatcher.Dispatch(context.Background(), keyedJobRequest("job-key", "/bin/false"))
	if changed.Error == nil || changed.Error.Code != "idempotency_conflict" {
		t.Fatalf("changed request = %#v", changed)
	}
	calls := readTestLines(t, fixture.callsPath)
	if len(calls) != 1 {
		t.Fatalf("systemd-run calls = %#v, want one", calls)
	}
	wantUnit := strings.TrimSuffix(jobUnit(strings.TrimPrefix(*first.Handle, JobHandlePrefix)), JobUnitSuffix)
	if calls[0] != wantUnit {
		t.Fatalf("launched unit = %q, want %q", calls[0], wantUnit)
	}
}

func TestKeyedJobStartLostSystemdRunResponseUsesExactUnitEvidence(t *testing.T) {
	fixture := newKeyedJobFixture(t, "lost")
	result := fixture.dispatcher.Dispatch(context.Background(), keyedJobRequest("lost-response", "/bin/true"))
	if !result.OK || result.Handle == nil {
		t.Fatalf("lost response result = %#v", result)
	}
	id := strings.TrimPrefix(*result.Handle, JobHandlePrefix)
	unit := jobUnit(id)
	if _, err := os.Stat(filepath.Join(fixture.stateDir, unit)); err != nil {
		t.Fatalf("exact accepted unit evidence missing: %v", err)
	}
	calls := readTestLines(t, fixture.callsPath)
	if len(calls) != 1 || calls[0]+JobUnitSuffix != unit {
		t.Fatalf("systemd-run calls = %#v, exact unit = %q", calls, unit)
	}
}

func TestKeyedJobStartCrashBeforeLaunchReusesPreallocatedExactUnit(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	request := keyedJobRequest("crash-before-launch", "/bin/true")
	normalized, requestHash, err := normalizeMutationRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	args, stdin, hasStdin, invalid, ok := decodeKeyedJobStart(normalized.Args)
	if !ok {
		t.Fatal(invalid)
	}
	if err := fixture.dispatcher.ensureJobDirectories(); err != nil {
		t.Fatal(err)
	}
	id, err := generateJobID(zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.dispatcher.prepareKeyedJobState(args, stdin, hasStdin, id); err != nil {
		t.Fatal(err)
	}
	record := keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       keyedStateInProgress,
		Operation:   "job.start",
		RequestHash: requestHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Job: &keyedJobRecord{
			ID:              id,
			Unit:            jobUnit(id),
			LaunchAttempted: false,
		},
	}
	if err := fixture.dispatcher.keyedState.write(digestString(request.IdempotencyKey), record); err != nil {
		t.Fatal(err)
	}

	result := fixture.dispatcher.Dispatch(context.Background(), request)
	if !result.OK || result.Handle == nil || *result.Handle != jobHandle(id) {
		t.Fatalf("crash recovery result = %#v, want %s", result, jobHandle(id))
	}
	calls := readTestLines(t, fixture.callsPath)
	wantUnit := strings.TrimSuffix(jobUnit(id), JobUnitSuffix)
	if len(calls) != 1 || calls[0] != wantUnit {
		t.Fatalf("calls = %#v, want exact unit %q", calls, wantUnit)
	}
}

func TestKeyedJobStartIncompletePrelaunchStateCreatesNoPhantom(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	request := keyedJobRequest("missing-native-state", "/bin/true")
	_, requestHash, err := normalizeMutationRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	id, err := generateJobID(zeroReader{})
	if err != nil {
		t.Fatal(err)
	}
	record := keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       keyedStateInProgress,
		Operation:   "job.start",
		RequestHash: requestHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Job:         &keyedJobRecord{ID: id, Unit: jobUnit(id), LaunchAttempted: false},
	}
	if err := fixture.dispatcher.keyedState.write(digestString(request.IdempotencyKey), record); err != nil {
		t.Fatal(err)
	}
	result := fixture.dispatcher.Dispatch(context.Background(), request)
	if result.OK || result.Error == nil || result.Error.Code != "uncertain_prior_execution" {
		t.Fatalf("incomplete state result = %#v", result)
	}
	if _, err := os.Stat(fixture.callsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("phantom state launched systemd-run: %v", err)
	}
}

func TestKeyedJobStartClearlyAbsentUnitRetriesOnlySameExactUnit(t *testing.T) {
	fixture := newKeyedJobFixture(t, "fail")
	oldWindow := keyedJobEvidenceWindow
	keyedJobEvidenceWindow = 15 * time.Millisecond
	defer func() { keyedJobEvidenceWindow = oldWindow }()

	result := fixture.dispatcher.Dispatch(context.Background(), keyedJobRequest("absent-unit", "/bin/true"))
	if result.OK || result.Error == nil || result.Error.Code != "job_start_failed" {
		t.Fatalf("absent unit result = %#v", result)
	}
	calls := readTestLines(t, fixture.callsPath)
	if len(calls) != 2 {
		t.Fatalf("systemd-run attempts = %#v, want two bounded attempts", calls)
	}
	if calls[0] != calls[1] {
		t.Fatalf("retry created a second unit: %#v", calls)
	}
}

func TestKeyedJobStartOrphanedLaunchAttemptWithIndeterminateInspectionIsUncertain(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	request := keyedJobRequest("indeterminate", "/bin/true")
	normalized, requestHash, err := normalizeMutationRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	args, stdin, hasStdin, invalid, ok := decodeKeyedJobStart(normalized.Args)
	if !ok {
		t.Fatal(invalid)
	}
	if err := fixture.dispatcher.ensureJobDirectories(); err != nil {
		t.Fatal(err)
	}
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.dispatcher.prepareKeyedJobState(args, stdin, hasStdin, id); err != nil {
		t.Fatal(err)
	}
	record := keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       keyedStateInProgress,
		Operation:   "job.start",
		RequestHash: requestHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Job:         &keyedJobRecord{ID: id, Unit: jobUnit(id), LaunchAttempted: true},
	}
	if err := fixture.dispatcher.keyedState.write(digestString(request.IdempotencyKey), record); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.dispatcher.systemctlPath, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	oldWindow := keyedJobEvidenceWindow
	keyedJobEvidenceWindow = 15 * time.Millisecond
	defer func() { keyedJobEvidenceWindow = oldWindow }()
	result := fixture.dispatcher.Dispatch(context.Background(), request)
	if result.OK || result.Error == nil || result.Error.Code != "uncertain_prior_execution" {
		t.Fatalf("indeterminate result = %#v", result)
	}
	if _, err := os.Stat(fixture.callsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("indeterminate state relaunched unit: %v", err)
	}
}

func readTestLines(t *testing.T, path string) []string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestKeyedJobStartMigratesMatchingLegacyKeyWithoutRelaunch(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	request := keyedJobRequest("legacy-accepted", "/bin/true")
	normalized, _, err := normalizeMutationRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	args, stdin, hasStdin, invalid, ok := decodeKeyedJobStart(normalized.Args)
	if !ok {
		t.Fatal(invalid)
	}
	if err := fixture.dispatcher.ensureJobDirectories(); err != nil {
		t.Fatal(err)
	}
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.dispatcher.prepareKeyedJobState(args, stdin, hasStdin, id); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.stateDir, jobUnit(id)), nil, 0600); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(fixture.dispatcher.jobKeysDir, digestString(request.IdempotencyKey))
	if err := writeFileDurable(legacyPath, []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result := fixture.dispatcher.Dispatch(context.Background(), request)
	if !result.OK || result.Handle == nil || *result.Handle != jobHandle(id) {
		t.Fatalf("legacy migration result = %#v", result)
	}
	if calls := readTestLines(t, fixture.callsPath); len(calls) != 0 {
		t.Fatalf("accepted legacy job was relaunched: %#v", calls)
	}
	record, found, err := fixture.dispatcher.keyedState.read(digestString(request.IdempotencyKey))
	if err != nil || !found || record.State != keyedStateCompleted || record.Job == nil || record.Job.ID != id {
		t.Fatalf("migrated record = %#v, found=%v, err=%v", record, found, err)
	}
}

func TestKeyedJobStartLegacyKeyBindsNormalizedRequest(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	original := keyedJobRequest("legacy-conflict", "/bin/true")
	normalized, _, err := normalizeMutationRequest(original)
	if err != nil {
		t.Fatal(err)
	}
	args, stdin, hasStdin, invalid, ok := decodeKeyedJobStart(normalized.Args)
	if !ok {
		t.Fatal(invalid)
	}
	if err := fixture.dispatcher.ensureJobDirectories(); err != nil {
		t.Fatal(err)
	}
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.dispatcher.prepareKeyedJobState(args, stdin, hasStdin, id); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(fixture.dispatcher.jobKeysDir, digestString(original.IdempotencyKey))
	if err := writeFileDurable(legacyPath, []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	result := fixture.dispatcher.Dispatch(context.Background(), keyedJobRequest("legacy-conflict", "/bin/false"))
	if result.Error == nil || result.Error.Code != "idempotency_conflict" {
		t.Fatalf("legacy changed request = %#v", result)
	}
	if calls := readTestLines(t, fixture.callsPath); len(calls) != 0 {
		t.Fatalf("conflicting legacy request launched work: %#v", calls)
	}
}

func TestKeyedJobStartLegacyMappingWithoutNativeStateIsUncertain(t *testing.T) {
	fixture := newKeyedJobFixture(t, "success")
	request := keyedJobRequest("legacy-incomplete", "/bin/true")
	id, err := newJobID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.dispatcher.jobKeysDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(fixture.dispatcher.jobKeysDir, digestString(request.IdempotencyKey))
	if err := writeFileDurable(legacyPath, []byte(id+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result := fixture.dispatcher.Dispatch(context.Background(), request)
	if result.OK || result.Error == nil || result.Error.Code != "uncertain_prior_execution" {
		t.Fatalf("legacy incomplete result = %#v", result)
	}
	if calls := readTestLines(t, fixture.callsPath); len(calls) != 0 {
		t.Fatalf("legacy incomplete state launched work: %#v", calls)
	}
}
