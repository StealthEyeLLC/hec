package hec

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type jobStartArgs struct {
	Command     *string           `json:"command"`
	Argv        []string          `json:"argv"`
	CWD         string            `json:"cwd"`
	Env         map[string]string `json:"env"`
	UnsetEnv    []string          `json:"unset_env"`
	Stdin       *string           `json:"stdin"`
	StdinBase64 *string           `json:"stdin_base64"`
	TimeoutMS   int64             `json:"timeout_ms"`
}

type jobHandleArgs struct {
	Handle string `json:"handle"`
}

type jobOutputArgs struct {
	Handle string `json:"handle"`
	Stream string `json:"stream"`
	Offset int64  `json:"offset"`
	Limit  *int64 `json:"limit"`
}

type jobWaitArgs struct {
	Handle    string `json:"handle"`
	TimeoutMS int64  `json:"timeout_ms"`
}

type jobSignalArgs struct {
	Handle string `json:"handle"`
	Signal string `json:"signal"`
}

type systemdJobState struct {
	LoadState      string
	ActiveState    string
	SubState       string
	Result         string
	ExecMainCode   string
	ExecMainStatus string
	Present        bool
}

func (d *Dispatcher) jobStart(ctx context.Context, raw map[string]any, idempotencyKey string) Result {
	var args jobStartArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("job.start", "invalid_arguments", err.Error())
	}
	if err := validateJobStartArgs(args); err != nil {
		return failedResult("job.start", "invalid_arguments", err.Error())
	}
	if len(idempotencyKey) > 512 {
		return failedResult("job.start", "invalid_arguments", "idempotency_key must not exceed 512 characters")
	}

	stdin, hasStdin, err := decodeJobStdin(args)
	if err != nil {
		return failedResult("job.start", "invalid_arguments", err.Error())
	}
	if err := d.ensureJobDirectories(); err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}

	if idempotencyKey == "" {
		id, err := d.createAndLaunchJob(ctx, args, stdin, hasStdin, "")
		if err != nil {
			return failedResult("job.start", "job_start_failed", err.Error())
		}
		return d.jobStartResult(ctx, id)
	}

	lock, err := lockJobKeys(d.jobKeysDir)
	if err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}
	defer unlockJobKeys(lock)

	mappingPath := filepath.Join(d.jobKeysDir, jobKeyDigest(idempotencyKey))
	mappedID, mapped, err := readJobKeyMapping(mappingPath)
	if err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}
	if mapped {
		info, inspectErr := d.inspectJob(ctx, mappedID)
		if inspectErr == nil {
			return jobStartResultFromInfo(info)
		}
		if !errors.Is(inspectErr, os.ErrNotExist) {
			return failedResult("job.start", "job_start_failed", inspectErr.Error())
		}
		_ = os.Remove(mappingPath)
	}

	id, err := d.createAndLaunchJob(ctx, args, stdin, hasStdin, mappingPath)
	if err != nil {
		return failedResult("job.start", "job_start_failed", err.Error())
	}
	return d.jobStartResult(ctx, id)
}

func validateJobStartArgs(args jobStartArgs) error {
	hasCommand := args.Command != nil
	hasArgv := args.Argv != nil
	if hasCommand == hasArgv {
		return errors.New("job.start requires exactly one of command or argv")
	}
	if hasArgv && (len(args.Argv) == 0 || args.Argv[0] == "") {
		return errors.New("argv must contain a nonempty executable name")
	}
	if args.CWD == "" {
		args.CWD = DefaultCWD
	}
	if strings.ContainsRune(args.CWD, '\x00') {
		return errors.New("cwd contains NUL")
	}
	if args.Stdin != nil && args.StdinBase64 != nil {
		return errors.New("job.start accepts only one of stdin or stdin_base64")
	}
	if _, err := durationFromMilliseconds(args.TimeoutMS); err != nil {
		return errors.New("timeout_ms must be greater than or equal to zero and within duration range")
	}
	if _, err := buildEnvironment(args.Env, args.UnsetEnv); err != nil {
		return err
	}
	return nil
}

func decodeJobStdin(args jobStartArgs) ([]byte, bool, error) {
	if args.Stdin != nil {
		return []byte(*args.Stdin), true, nil
	}
	if args.StdinBase64 == nil {
		return nil, false, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(*args.StdinBase64)
	if err != nil {
		return nil, false, fmt.Errorf("stdin_base64 is not valid base64: %w", err)
	}
	return decoded, true, nil
}

func (d *Dispatcher) ensureJobDirectories() error {
	if err := os.MkdirAll(d.jobsDir, 0700); err != nil {
		return fmt.Errorf("create job directory root: %w", err)
	}
	if err := os.Chmod(d.jobsDir, 0700); err != nil {
		return fmt.Errorf("set job directory root permissions: %w", err)
	}
	if err := os.MkdirAll(d.jobKeysDir, 0700); err != nil {
		return fmt.Errorf("create job key directory: %w", err)
	}
	if err := os.Chmod(d.jobKeysDir, 0700); err != nil {
		return fmt.Errorf("set job key directory permissions: %w", err)
	}
	return nil
}

func (d *Dispatcher) createAndLaunchJob(ctx context.Context, args jobStartArgs, stdin []byte, hasStdin bool, mappingPath string) (string, error) {
	id, err := newJobID()
	if err != nil {
		return "", err
	}
	jobDir := filepath.Join(d.jobsDir, id)
	if err := os.Mkdir(jobDir, 0700); err != nil {
		return "", fmt.Errorf("create job directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(jobDir)
			if mappingPath != "" {
				_ = os.Remove(mappingPath)
			}
		}
	}()

	stdinPath := ""
	if hasStdin {
		stdinPath = filepath.Join(jobDir, "stdin")
		if err := writeBytesAtomic(stdinPath, stdin, 0600); err != nil {
			return "", fmt.Errorf("write job stdin: %w", err)
		}
	}
	stdoutPath := filepath.Join(jobDir, "stdout")
	stderrPath := filepath.Join(jobDir, "stderr")
	if err := createPrivateFile(stdoutPath); err != nil {
		return "", fmt.Errorf("create job stdout: %w", err)
	}
	if err := createPrivateFile(stderrPath); err != nil {
		return "", fmt.Errorf("create job stderr: %w", err)
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
	specPath := filepath.Join(jobDir, "spec.json")
	if err := writeJSONAtomic(specPath, spec, 0600); err != nil {
		return "", fmt.Errorf("write job specification: %w", err)
	}
	if mappingPath != "" {
		if err := writeTextAtomic(mappingPath, id+"\n", 0600); err != nil {
			return "", fmt.Errorf("write idempotency mapping: %w", err)
		}
	}
	if err := d.launchJob(ctx, id); err != nil {
		return "", err
	}
	cleanup = false
	return id, nil
}

func createPrivateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return file.Close()
}

func lockJobKeys(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func unlockJobKeys(lock *os.File) {
	if lock == nil {
		return
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func readJobKeyMapping(path string) (string, bool, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	id := strings.TrimSpace(string(payload))
	if _, err := parseJobHandle(jobHandle(id)); err != nil {
		return "", false, fmt.Errorf("invalid idempotency mapping: %w", err)
	}
	return id, true, nil
}

func (d *Dispatcher) launchJob(ctx context.Context, id string) error {
	jobDir := filepath.Join(d.jobsDir, id)
	unit := jobUnit(id)
	arguments := []string{
		"--quiet",
		"--unit=" + strings.TrimSuffix(unit, JobUnitSuffix),
		"--property=Type=exec",
		"--property=KillMode=mixed",
		"--property=StandardOutput=append:" + filepath.Join(jobDir, "stdout"),
		"--property=StandardError=append:" + filepath.Join(jobDir, "stderr"),
		"--",
		d.hecBinaryPath,
		"job-run",
		filepath.Join(jobDir, "spec.json"),
	}
	command := exec.CommandContext(ctx, d.systemdRunPath, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("start transient job unit: %s", message)
	}
	return nil
}

func (d *Dispatcher) jobStartResult(ctx context.Context, id string) Result {
	info, err := d.inspectJob(ctx, id)
	if err != nil {
		info = jobInfo{ID: id, Handle: jobHandle(id), Unit: jobUnit(id), JobStatus: JobStatusRunning, Directory: true}
	}
	result := jobStartResultFromInfo(info)
	result.Status = StatusRunning
	return result
}

func jobStartResultFromInfo(info jobInfo) Result {
	result := newResult("job.start")
	result.OK = true
	result.Status = info.JobStatus
	result.Handle = &info.Handle
	result.Result = map[string]any{
		"id":         info.ID,
		"handle":     info.Handle,
		"unit":       info.Unit,
		"job_status": info.JobStatus,
	}
	return result
}

func (d *Dispatcher) jobStatus(ctx context.Context, raw map[string]any) Result {
	var args jobHandleArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("job.status", "invalid_arguments", err.Error())
	}
	id, err := parseJobHandle(args.Handle)
	if err != nil {
		return failedResult("job.status", "invalid_arguments", err.Error())
	}
	info, err := d.inspectJob(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return jobNotFoundResult("job.status", args.Handle)
	}
	if err != nil {
		return failedResult("job.status", "job_status_failed", err.Error())
	}
	result := newResult("job.status")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = jobInfoResult(info)
	return result
}

func jobInfoResult(info jobInfo) map[string]any {
	result := map[string]any{
		"id":           info.ID,
		"handle":       info.Handle,
		"unit":         info.Unit,
		"job_status":   info.JobStatus,
		"stdout_bytes": info.StdoutBytes,
		"stderr_bytes": info.StderrBytes,
	}
	if info.ActiveState != "" {
		result["active_state"] = info.ActiveState
	}
	if info.SubState != "" {
		result["sub_state"] = info.SubState
	}
	if info.ExitCode != nil {
		result["exit_code"] = *info.ExitCode
	}
	if info.Signal != nil {
		result["signal"] = *info.Signal
	}
	if info.TimedOut != nil {
		result["timed_out"] = *info.TimedOut
	}
	return result
}

func (d *Dispatcher) inspectJob(ctx context.Context, id string) (jobInfo, error) {
	info := jobInfo{ID: id, Handle: jobHandle(id), Unit: jobUnit(id), JobStatus: JobStatusUnknown}
	jobDir := filepath.Join(d.jobsDir, id)
	if stat, err := os.Stat(jobDir); err == nil && stat.IsDir() {
		info.Directory = true
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return jobInfo{}, err
	}

	state, stateErr := d.readSystemdJobState(ctx, info.Unit)
	if stateErr != nil {
		return jobInfo{}, stateErr
	}
	info.UnitPresent = state.Present
	info.ActiveState = state.ActiveState
	info.SubState = state.SubState
	info.SystemdResult = state.Result

	if !info.Directory && !info.UnitPresent {
		return jobInfo{}, os.ErrNotExist
	}
	if info.Directory {
		info.StdoutBytes = fileSize(filepath.Join(jobDir, "stdout"))
		info.StderrBytes = fileSize(filepath.Join(jobDir, "stderr"))
		final, err := readJobFinalResult(filepath.Join(jobDir, "result.json"))
		if err == nil {
			info.JobStatus = final.Status
			info.ExitCode = final.ExitCode
			info.Signal = final.Signal
			info.TimedOut = &final.TimedOut
			return info, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return jobInfo{}, err
		}
	}

	switch state.ActiveState {
	case "activating":
		info.JobStatus = JobStatusStarting
	case "active", "reloading", "deactivating":
		info.JobStatus = JobStatusRunning
	case "failed":
		info.JobStatus = JobStatusFailed
	case "inactive":
		if state.Result == "success" && state.ExecMainCode == "1" && state.ExecMainStatus == "0" {
			info.JobStatus = JobStatusCompleted
		} else if state.Result != "" && state.Result != "success" {
			info.JobStatus = JobStatusFailed
		}
	}
	return info, nil
}

func fileSize(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return stat.Size()
}

func (d *Dispatcher) readSystemdJobState(ctx context.Context, unit string) (systemdJobState, error) {
	command := exec.CommandContext(ctx, d.systemctlPath,
		"show", "--no-pager",
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--property=Result",
		"--property=ExecMainCode",
		"--property=ExecMainStatus",
		unit,
	)
	output, err := command.CombinedOutput()
	state := systemdJobState{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			state.LoadState = value
		case "ActiveState":
			state.ActiveState = value
		case "SubState":
			state.SubState = value
		case "Result":
			state.Result = value
		case "ExecMainCode":
			state.ExecMainCode = value
		case "ExecMainStatus":
			state.ExecMainStatus = value
		}
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return systemdJobState{}, fmt.Errorf("read systemd job state: %s", message)
	}
	if state.LoadState == "not-found" || state.LoadState == "" {
		state.Present = false
		return state, nil
	}
	state.Present = true
	return state, nil
}

func (d *Dispatcher) jobOutput(ctx context.Context, raw map[string]any) Result {
	var args jobOutputArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("job.output", "invalid_arguments", err.Error())
	}
	id, err := parseJobHandle(args.Handle)
	if err != nil {
		return failedResult("job.output", "invalid_arguments", err.Error())
	}
	if args.Stream != "stdout" && args.Stream != "stderr" {
		return failedResult("job.output", "invalid_arguments", "stream must be stdout or stderr")
	}
	if args.Offset < 0 {
		return failedResult("job.output", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	limit := DefaultJobOutputSize
	if args.Limit != nil {
		if *args.Limit <= 0 {
			return failedResult("job.output", "invalid_arguments", "limit must be greater than zero")
		}
		limit = *args.Limit
	}
	if limit > MaxJobOutputSize {
		return failedResult("job.output", "invalid_arguments", fmt.Sprintf("limit must not exceed %d", MaxJobOutputSize))
	}
	if _, err := d.inspectJob(ctx, id); errors.Is(err, os.ErrNotExist) {
		return jobNotFoundResult("job.output", args.Handle)
	} else if err != nil {
		return failedResult("job.output", "job_output_failed", err.Error())
	}
	path := filepath.Join(d.jobsDir, id, args.Stream)
	chunk, total, next, eof, err := readOutputRange(path, args.Offset, limit)
	if err != nil {
		return failedResult("job.output", "job_output_failed", err.Error())
	}
	encoded, encoding := encodeOutput(chunk)
	result := newResult("job.output")
	result.OK = true
	result.Handle = &args.Handle
	if args.Stream == "stdout" {
		result.Stdout = encoded
		result.StdoutEncoding = encoding
	} else {
		result.Stderr = encoded
		result.StderrEncoding = encoding
	}
	result.Result = map[string]any{
		"stream":      args.Stream,
		"offset":      args.Offset,
		"next_offset": next,
		"total_bytes": total,
		"eof":         eof,
		"encoding":    encoding,
	}
	return result
}

func readOutputRange(path string, offset, limit int64) ([]byte, int64, int64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, offset, false, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, offset, false, err
	}
	total := stat.Size()
	if offset >= total {
		return []byte{}, total, offset, true, nil
	}
	remaining := total - offset
	if limit > remaining {
		limit = remaining
	}
	buffer := make([]byte, int(limit))
	read, err := file.ReadAt(buffer, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, offset, false, err
	}
	buffer = buffer[:read]
	next := offset + int64(read)
	return buffer, total, next, next >= total, nil
}

func (d *Dispatcher) jobWait(ctx context.Context, raw map[string]any) Result {
	var args jobWaitArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("job.wait", "invalid_arguments", err.Error())
	}
	id, err := parseJobHandle(args.Handle)
	if err != nil {
		return failedResult("job.wait", "invalid_arguments", err.Error())
	}
	if args.TimeoutMS < 0 {
		return failedResult("job.wait", "invalid_arguments", "timeout_ms must be greater than or equal to zero")
	}
	waitBudget, err := jobWaitTimeout(args.TimeoutMS)
	if err != nil {
		return failedResult("job.wait", "invalid_arguments", "timeout_ms must be between 0 and 15000")
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < waitBudget {
			waitBudget = remaining
		}
	}

	for {
		info, inspectErr := d.inspectJob(ctx, id)
		if errors.Is(inspectErr, os.ErrNotExist) {
			return jobNotFoundResult("job.wait", args.Handle)
		}
		if inspectErr != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return failedResult("job.wait", "canceled", "job wait was canceled")
			}
			return failedResult("job.wait", "job_wait_failed", inspectErr.Error())
		}
		if isTerminalJobStatus(info.JobStatus) {
			return jobWaitResult(args.Handle, info, false)
		}
		if waitBudget <= 0 {
			return jobWaitResult(args.Handle, info, true)
		}

		wait := 200 * time.Millisecond
		if waitBudget < wait {
			wait = waitBudget
		}
		started := time.Now()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return failedResult("job.wait", "canceled", "job wait was canceled")
			}
			return jobWaitResult(args.Handle, info, true)
		case <-timer.C:
			waitBudget -= time.Since(started)
		}
	}
}

func jobWaitResult(handle string, info jobInfo, waitTimedOut bool) Result {
	result := newResult("job.wait")
	result.OK = true
	result.Handle = &handle
	result.Result = jobInfoResult(info)
	result.Result["wait_timed_out"] = waitTimedOut
	return result
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case JobStatusCompleted, JobStatusFailed, JobStatusTimedOut, JobStatusCancelled:
		return true
	default:
		return false
	}
}

func (d *Dispatcher) jobSignal(ctx context.Context, raw map[string]any) Result {
	var args jobSignalArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("job.signal", "invalid_arguments", err.Error())
	}
	id, err := parseJobHandle(args.Handle)
	if err != nil {
		return failedResult("job.signal", "invalid_arguments", err.Error())
	}
	linuxSignal, signalName, err := parseLinuxSignalName(args.Signal)
	if err != nil {
		return failedResult("job.signal", "invalid_arguments", err.Error())
	}
	info, err := d.inspectJob(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return jobNotFoundResult("job.signal", args.Handle)
	}
	if err != nil {
		return failedResult("job.signal", "job_signal_failed", err.Error())
	}
	if info.JobStatus != JobStatusStarting && info.JobStatus != JobStatusRunning {
		return failedResult("job.signal", "job_not_running", fmt.Sprintf("%s is not running", args.Handle))
	}
	whom := "main"
	if linuxSignal == syscall.SIGKILL || linuxSignal == syscall.SIGSTOP || linuxSignal == syscall.SIGCHLD || linuxSignal == syscall.SIGURG {
		whom = "all"
	}
	command := exec.CommandContext(ctx, d.systemctlPath, "kill", "--kill-whom="+whom, "--signal="+signalName, info.Unit)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return failedResult("job.signal", "job_signal_failed", message)
	}
	result := newResult("job.signal")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"id":         id,
		"handle":     args.Handle,
		"unit":       info.Unit,
		"job_status": info.JobStatus,
		"signal":     signalName,
	}
	return result
}

func (d *Dispatcher) jobList(ctx context.Context, raw map[string]any) Result {
	if len(raw) != 0 {
		return failedResult("job.list", "invalid_arguments", "job.list does not accept arguments")
	}
	ids, err := d.listJobIDs(ctx)
	if err != nil {
		return failedResult("job.list", "job_list_failed", err.Error())
	}
	entries := make([]any, 0, len(ids))
	for _, id := range ids {
		info, inspectErr := d.inspectJob(ctx, id)
		if errors.Is(inspectErr, os.ErrNotExist) {
			continue
		}
		if inspectErr != nil {
			return failedResult("job.list", "job_list_failed", inspectErr.Error())
		}
		entry := map[string]any{
			"id":           info.ID,
			"handle":       info.Handle,
			"unit":         info.Unit,
			"job_status":   info.JobStatus,
			"stdout_bytes": info.StdoutBytes,
			"stderr_bytes": info.StderrBytes,
		}
		if info.ExitCode != nil {
			entry["exit_code"] = *info.ExitCode
		}
		if info.Signal != nil {
			entry["signal"] = *info.Signal
		}
		entries = append(entries, entry)
	}
	result := newResult("job.list")
	result.OK = true
	result.Result = map[string]any{"jobs": entries, "count": len(entries)}
	return result
}

func (d *Dispatcher) listJobIDs(ctx context.Context) ([]string, error) {
	ids := make(map[string]struct{})
	entries, err := os.ReadDir(d.jobsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, err := parseJobHandle(jobHandle(id)); err == nil {
			ids[id] = struct{}{}
		}
	}
	command := exec.CommandContext(ctx, d.systemctlPath, "list-units", "--all", "--type=service", "--no-legend", "--plain", JobUnitPrefix+"*"+JobUnitSuffix)
	output, commandErr := command.CombinedOutput()
	if commandErr != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return nil, fmt.Errorf("list systemd jobs: %s", message)
		}
		return nil, fmt.Errorf("list systemd jobs: %w", commandErr)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := fields[0]
		if !strings.HasPrefix(unit, JobUnitPrefix) || !strings.HasSuffix(unit, JobUnitSuffix) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(unit, JobUnitPrefix), JobUnitSuffix)
		if _, err := parseJobHandle(jobHandle(id)); err == nil {
			ids[id] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	return ordered, nil
}

func (d *Dispatcher) jobForget(ctx context.Context, raw map[string]any) Result {
	var args jobHandleArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("job.forget", "invalid_arguments", err.Error())
	}
	id, err := parseJobHandle(args.Handle)
	if err != nil {
		return failedResult("job.forget", "invalid_arguments", err.Error())
	}
	info, err := d.inspectJob(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return jobNotFoundResult("job.forget", args.Handle)
	}
	if err != nil {
		return failedResult("job.forget", "job_forget_failed", err.Error())
	}
	if !isTerminalJobStatus(info.JobStatus) {
		return failedResult("job.forget", "job_not_finished", fmt.Sprintf("%s is not finished", args.Handle))
	}
	if info.ActiveState == "failed" {
		command := exec.CommandContext(ctx, d.systemctlPath, "reset-failed", info.Unit)
		output, resetErr := command.CombinedOutput()
		if resetErr != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = resetErr.Error()
			}
			return failedResult("job.forget", "job_forget_failed", message)
		}
	}
	if err := d.removeJobKeyMappings(id); err != nil {
		return failedResult("job.forget", "job_forget_failed", err.Error())
	}
	if err := os.RemoveAll(filepath.Join(d.jobsDir, id)); err != nil {
		return failedResult("job.forget", "job_forget_failed", err.Error())
	}
	result := newResult("job.forget")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{"id": id, "handle": args.Handle, "forgotten": true}
	return result
}

func (d *Dispatcher) removeJobKeyMappings(id string) error {
	lock, err := lockJobKeys(d.jobKeysDir)
	if err != nil {
		return err
	}
	entries, readErr := os.ReadDir(d.jobKeysDir)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		unlockJobKeys(lock)
		return readErr
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".lock" || len(entry.Name()) != 64 {
			continue
		}
		path := filepath.Join(d.jobKeysDir, entry.Name())
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			unlockJobKeys(lock)
			return readErr
		}
		if strings.TrimSpace(string(payload)) == id {
			if err := removeFileDurable(path); err != nil {
				unlockJobKeys(lock)
				return err
			}
		}
	}
	unlockJobKeys(lock)

	store := d.keyedState
	if store == nil || store.root != d.mutationKeysDir {
		store = newKeyedMutationStore(d.mutationKeysDir)
		d.keyedState = store
	}
	stateEntries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range stateEntries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		digest := strings.TrimSuffix(name, ".json")
		if store.isActive(digest) {
			continue
		}
		stateLock, err := store.lock(digest)
		if err != nil {
			return err
		}
		record, found, readErr := store.read(digest)
		if readErr != nil {
			unlockKeyedMutation(stateLock)
			return readErr
		}
		if found && record.Operation == "job.start" && record.Job != nil && record.Job.ID == id {
			if err := removeFileDurable(store.statePath(digest)); err != nil {
				unlockKeyedMutation(stateLock)
				return err
			}
		}
		unlockKeyedMutation(stateLock)
	}
	return nil
}

func jobNotFoundResult(operation, handle string) Result {
	return failedResult(operation, "job_not_found", fmt.Sprintf("%s was not found", handle))
}

func resultInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
