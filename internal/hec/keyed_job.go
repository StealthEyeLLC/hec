package hec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var keyedJobEvidenceWindow = 2 * time.Second

func (d *Dispatcher) executeNewKeyedJobStart(ctx context.Context, request CallRequest, keyDigest string, record keyedMutationRecord) Result {
	args, stdin, hasStdin, validationResult, ok := decodeKeyedJobStart(request.Args)
	if !ok {
		return validationResult
	}
	if err := d.ensureJobDirectories(); err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}

	id, err := newJobID()
	if err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}
	if err := d.prepareKeyedJobState(args, stdin, hasStdin, id); err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}

	record.Job = &keyedJobRecord{ID: id, Unit: jobUnit(id), LaunchAttempted: false}
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.keyedState.write(keyDigest, record); err != nil {
		_ = os.RemoveAll(filepath.Join(d.jobsDir, id))
		return failedResult("job.start", "idempotency_state_failed", err.Error())
	}
	return d.launchAndCompleteKeyedJob(ctx, keyDigest, record, true)
}

func (d *Dispatcher) reconcileKeyedJobStart(ctx context.Context, request CallRequest, keyDigest string, record keyedMutationRecord) Result {
	_, _, _, validationResult, ok := decodeKeyedJobStart(request.Args)
	if !ok {
		return validationResult
	}
	if err := validateKeyedJobRecord(record.Job); err != nil {
		return failedResult("job.start", "idempotency_state_corrupt", err.Error())
	}
	if _, err := os.Stat(filepath.Join(d.jobsDir, record.Job.ID, "spec.json")); err != nil {
		return failedResult("job.start", "uncertain_prior_execution", "recorded job state is incomplete; prior execution cannot be proven safe to repeat")
	}

	if record.Job.LaunchAttempted {
		info, accepted, absent, err := d.waitForKeyedJobEvidence(ctx, record.Job.ID)
		if err != nil {
			return failedResult("job.start", "uncertain_prior_execution", "the exact recorded systemd unit could not be resolved")
		}
		if accepted {
			return d.completeKeyedJob(keyDigest, record, jobStartResultFromInfo(info))
		}
		if !absent {
			return failedResult("job.start", "uncertain_prior_execution", "the exact recorded systemd unit has indeterminate state")
		}
	}
	return d.launchAndCompleteKeyedJob(ctx, keyDigest, record, true)
}

func decodeKeyedJobStart(raw map[string]any) (jobStartArgs, []byte, bool, Result, bool) {
	var args jobStartArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return args, nil, false, failedResult("job.start", "invalid_arguments", err.Error()), false
	}
	if err := validateJobStartArgs(args); err != nil {
		return args, nil, false, failedResult("job.start", "invalid_arguments", err.Error()), false
	}
	stdin, hasStdin, err := decodeJobStdin(args)
	if err != nil {
		return args, nil, false, failedResult("job.start", "invalid_arguments", err.Error()), false
	}
	return args, stdin, hasStdin, Result{}, true
}

func validateKeyedJobRecord(job *keyedJobRecord) error {
	if job == nil {
		return errors.New("keyed job state is missing the allocated job")
	}
	if _, err := parseJobHandle(jobHandle(job.ID)); err != nil {
		return fmt.Errorf("keyed job state has an invalid job ID: %w", err)
	}
	if job.Unit != jobUnit(job.ID) {
		return errors.New("keyed job state has an invalid exact systemd unit")
	}
	return nil
}

func (d *Dispatcher) prepareKeyedJobState(args jobStartArgs, stdin []byte, hasStdin bool, id string) (resultErr error) {
	jobDir := filepath.Join(d.jobsDir, id)
	if err := os.Mkdir(jobDir, 0700); err != nil {
		return fmt.Errorf("create keyed job directory: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = os.RemoveAll(jobDir)
		}
	}()

	stdinPath := ""
	if hasStdin {
		stdinPath = filepath.Join(jobDir, "stdin")
		if err := writeFileDurable(stdinPath, stdin, 0600); err != nil {
			return fmt.Errorf("write keyed job stdin: %w", err)
		}
	}
	stdoutPath := filepath.Join(jobDir, "stdout")
	stderrPath := filepath.Join(jobDir, "stderr")
	if err := createAndSyncPrivateFile(stdoutPath); err != nil {
		return fmt.Errorf("create keyed job stdout: %w", err)
	}
	if err := createAndSyncPrivateFile(stderrPath); err != nil {
		return fmt.Errorf("create keyed job stderr: %w", err)
	}

	cwd := args.CWD
	if cwd == "" {
		cwd = DefaultCWD
	}
	spec := jobSpec{
		Command:   args.Command,
		Argv:      args.Argv,
		CWD:       cwd,
		Env:       args.Env,
		UnsetEnv:  args.UnsetEnv,
		StdinPath: stdinPath,
		TimeoutMS: args.TimeoutMS,
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode keyed job specification: %w", err)
	}
	if err := writeFileDurable(filepath.Join(jobDir, "spec.json"), payload, 0600); err != nil {
		return fmt.Errorf("write keyed job specification: %w", err)
	}
	if err := syncDirectory(jobDir); err != nil {
		return fmt.Errorf("sync keyed job directory: %w", err)
	}
	if err := syncDirectory(d.jobsDir); err != nil {
		return fmt.Errorf("sync job root directory: %w", err)
	}
	return nil
}

func createAndSyncPrivateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func (d *Dispatcher) launchAndCompleteKeyedJob(ctx context.Context, keyDigest string, record keyedMutationRecord, allowRetry bool) Result {
	if err := validateKeyedJobRecord(record.Job); err != nil {
		return failedResult("job.start", "idempotency_state_corrupt", err.Error())
	}
	if !record.Job.LaunchAttempted {
		record.Job.LaunchAttempted = true
		record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := d.keyedState.write(keyDigest, record); err != nil {
			return failedResult("job.start", "idempotency_state_failed", err.Error())
		}
	}

	attempts := 1
	if allowRetry {
		attempts = 2
	}
	var lastLaunchErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx.Err() != nil {
			return failedResult("job.start", "canceled", "job submission was canceled before the exact unit could be resolved")
		}
		launchErr := d.launchJob(ctx, record.Job.ID)
		if launchErr == nil {
			return d.completeKeyedJob(keyDigest, record, d.jobStartResult(ctx, record.Job.ID))
		}
		lastLaunchErr = launchErr

		info, accepted, absent, evidenceErr := d.waitForKeyedJobEvidence(ctx, record.Job.ID)
		if evidenceErr != nil {
			return failedResult("job.start", "uncertain_prior_execution", "systemd-run did not return cleanly and the exact recorded unit could not be resolved")
		}
		if accepted {
			return d.completeKeyedJob(keyDigest, record, jobStartResultFromInfo(info))
		}
		if !absent {
			return failedResult("job.start", "uncertain_prior_execution", "systemd-run did not return cleanly and the exact recorded unit is indeterminate")
		}
	}

	result := failedResult("job.start", "job_start_failed", lastLaunchErr.Error())
	return d.completeKeyedJob(keyDigest, record, result)
}

func (d *Dispatcher) waitForKeyedJobEvidence(ctx context.Context, id string) (jobInfo, bool, bool, error) {
	window := keyedJobEvidenceWindow
	if window <= 0 {
		window = 2 * time.Second
	}
	evidenceCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	var lastErr error
	for {
		info, accepted, absent, err := d.keyedJobEvidence(evidenceCtx, id)
		if err == nil && accepted {
			return info, true, false, nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-evidenceCtx.Done():
			if errors.Is(evidenceCtx.Err(), context.DeadlineExceeded) && lastErr == nil && absent {
				return info, false, true, nil
			}
			if lastErr != nil {
				return jobInfo{}, false, false, lastErr
			}
			return jobInfo{}, false, false, evidenceCtx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (d *Dispatcher) keyedJobEvidence(ctx context.Context, id string) (jobInfo, bool, bool, error) {
	info, err := d.inspectJob(ctx, id)
	if err != nil {
		return jobInfo{}, false, false, err
	}
	if info.UnitPresent || isTerminalJobStatus(info.JobStatus) || info.JobStatus == JobStatusStarting || info.JobStatus == JobStatusRunning {
		return info, true, false, nil
	}
	if info.Directory && info.JobStatus == JobStatusUnknown {
		return info, false, true, nil
	}
	return info, false, false, nil
}

func (d *Dispatcher) completeKeyedJob(keyDigest string, record keyedMutationRecord, result Result) Result {
	record.State = keyedStateCompleted
	record.Result = &result
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.keyedState.write(keyDigest, record); err != nil {
		return failedResult("job.start", "idempotency_state_failed", "job state was resolved but keyed completion state could not be published")
	}
	d.keyedState.pruneBestEffort(keyDigest)
	return result
}

func (d *Dispatcher) importLegacyJobStart(ctx context.Context, request CallRequest, keyDigest, requestHash string) (keyedMutationRecord, bool, Result, bool) {
	var empty keyedMutationRecord
	if strings.TrimSpace(d.jobKeysDir) == "" {
		return empty, false, Result{}, false
	}
	legacyLock, err := lockJobKeys(d.jobKeysDir)
	if err != nil {
		return empty, false, failedResult("job.start", "idempotency_state_failed", err.Error()), true
	}
	defer unlockJobKeys(legacyLock)

	legacyPath := filepath.Join(d.jobKeysDir, keyDigest)
	id, found, err := readJobKeyMapping(legacyPath)
	if err != nil {
		return empty, false, failedResult("job.start", "idempotency_state_corrupt", "legacy job key state is corrupt or unreadable"), true
	}
	if !found {
		return empty, false, Result{}, false
	}
	jobDirectory := filepath.Join(d.jobsDir, id)
	legacyHash, err := d.normalizedLegacyJobRequestHash(jobDirectory)
	if err != nil {
		return empty, false, failedResult("job.start", "uncertain_prior_execution", "legacy job key exists but its native request state is incomplete or unreadable"), true
	}
	if legacyHash != requestHash {
		return empty, false, failedResult("job.start", "idempotency_conflict", "idempotency key was already used for a different normalized job request"), true
	}

	record := keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       keyedStateInProgress,
		Operation:   request.Operation,
		RequestHash: requestHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Job: &keyedJobRecord{
			ID:              id,
			Unit:            jobUnit(id),
			LaunchAttempted: true,
		},
	}
	info, inspectErr := d.inspectJob(ctx, id)
	if inspectErr != nil && !errors.Is(inspectErr, os.ErrNotExist) {
		return empty, false, failedResult("job.start", "uncertain_prior_execution", "legacy job key exists but the exact recorded unit could not be inspected"), true
	}
	if inspectErr == nil && (info.UnitPresent || isTerminalJobStatus(info.JobStatus) || info.JobStatus == JobStatusStarting || info.JobStatus == JobStatusRunning) {
		result := jobStartResultFromInfo(info)
		record.State = keyedStateCompleted
		record.Result = &result
		if err := d.keyedState.write(keyDigest, record); err != nil {
			return empty, false, failedResult("job.start", "idempotency_state_failed", err.Error()), true
		}
		return empty, false, result, true
	}
	if err := d.keyedState.write(keyDigest, record); err != nil {
		return empty, false, failedResult("job.start", "idempotency_state_failed", err.Error()), true
	}
	return record, true, Result{}, false
}

func (d *Dispatcher) normalizedLegacyJobRequestHash(jobDirectory string) (string, error) {
	specPath := filepath.Join(jobDirectory, "spec.json")
	spec, err := decodeJobSpec(specPath)
	if err != nil {
		return "", err
	}
	raw := map[string]any{"cwd": spec.CWD}
	if spec.Command != nil {
		raw["command"] = *spec.Command
	} else {
		raw["argv"] = append([]string(nil), spec.Argv...)
	}
	if len(spec.Env) > 0 {
		raw["env"] = spec.Env
	}
	if len(spec.UnsetEnv) > 0 {
		raw["unset_env"] = append([]string(nil), spec.UnsetEnv...)
	}
	if spec.TimeoutMS != 0 {
		raw["timeout_ms"] = spec.TimeoutMS
	}
	if spec.StdinPath != "" {
		expected := filepath.Join(jobDirectory, "stdin")
		if filepath.Clean(spec.StdinPath) != filepath.Clean(expected) {
			return "", errors.New("legacy job stdin path is not the recorded job stdin")
		}
		stdin, err := os.ReadFile(spec.StdinPath)
		if err != nil {
			return "", err
		}
		if len(stdin) > 0 {
			raw["stdin_base64"] = base64.StdEncoding.EncodeToString(stdin)
		}
	}
	_, requestHash, err := normalizeMutationRequest(CallRequest{Operation: "job.start", Args: raw})
	return requestHash, err
}
