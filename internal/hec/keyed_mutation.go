package hec

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	hecschemas "github.com/StealthEyeLLC/hec/schemas"
	"github.com/google/jsonschema-go/jsonschema"
)

const (
	MutationKeyRootDir        = "/var/lib/hec/mutation-keys"
	keyedMutationStateVersion = 1
	keyedMutationMaxRecords   = 4096
	keyedMutationRetention    = 30 * 24 * time.Hour
	keyedMutationMaxFileBytes = 4 << 20
)

const (
	keyedStateInProgress = "in_progress"
	keyedStateCompleted  = "completed"
)

type keyedMutationRecord struct {
	Version     int             `json:"version"`
	State       string          `json:"state"`
	Operation   string          `json:"operation"`
	RequestHash string          `json:"request_hash"`
	UpdatedAt   string          `json:"updated_at"`
	Result      *Result         `json:"result,omitempty"`
	Job         *keyedJobRecord `json:"job,omitempty"`
}

type keyedJobRecord struct {
	ID              string `json:"id"`
	Unit            string `json:"unit"`
	LaunchAttempted bool   `json:"launch_attempted"`
}

type keyedMutationStore struct {
	root string

	activeMu sync.Mutex
	active   map[string]struct{}
}

func newKeyedMutationStore(root string) *keyedMutationStore {
	return &keyedMutationStore{root: root, active: make(map[string]struct{})}
}

func isMutationOperation(operation string) bool {
	switch operation {
	case "run",
		"job.start", "job.signal", "job.forget",
		"terminal.open", "terminal.write", "terminal.resize", "terminal.signal", "terminal.close",
		"file.write", "file.append", "file.patch", "file.remove",
		"upload.begin", "upload.chunk", "upload.finish", "upload.abort",
		"artifact.return", "artifact.materialize", "artifact.delete":
		return true
	default:
		return false
	}
}

func (d *Dispatcher) dispatchKeyedMutation(ctx context.Context, request CallRequest) Result {
	normalized, requestHash, err := normalizeMutationRequest(request)
	if err != nil {
		return failedResult(request.Operation, "invalid_arguments", err.Error())
	}
	store := d.keyedState
	if store == nil || store.root != d.mutationKeysDir {
		store = newKeyedMutationStore(d.mutationKeysDir)
		d.keyedState = store
	}
	keyDigest := digestString(request.IdempotencyKey)
	if !store.activate(keyDigest) {
		return failedResult(request.Operation, "idempotency_in_progress", "a request with this idempotency key is active")
	}
	defer store.deactivate(keyDigest)

	lock, err := store.lock(keyDigest)
	if err != nil {
		return failedResult(request.Operation, "idempotency_state_failed", err.Error())
	}
	defer unlockKeyedMutation(lock)

	record, found, err := store.read(keyDigest)
	if err != nil {
		return failedResult(request.Operation, "idempotency_state_corrupt", "keyed mutation state is corrupt or unreadable")
	}
	if !found && request.Operation == "job.start" {
		importedRecord, imported, importResult, handled := d.importLegacyJobStart(ctx, normalized, keyDigest, requestHash)
		if handled {
			return importResult
		}
		if imported {
			record = importedRecord
			found = true
		}
	}
	if found {
		if record.RequestHash != requestHash || record.Operation != request.Operation {
			return failedResult(request.Operation, "idempotency_conflict", "idempotency key was already used for a different normalized request")
		}
		switch record.State {
		case keyedStateCompleted:
			if record.Result == nil {
				return failedResult(request.Operation, "idempotency_state_corrupt", "completed keyed mutation state has no result")
			}
			return rehydrateStoredResult(*record.Result)
		case keyedStateInProgress:
			if request.Operation == "job.start" {
				return d.reconcileKeyedJobStart(ctx, normalized, keyDigest, record)
			}
			return failedResult(request.Operation, "uncertain_prior_execution", "a prior keyed mutation may have executed before completion was recorded")
		default:
			return failedResult(request.Operation, "idempotency_state_corrupt", "keyed mutation state is unknown")
		}
	}

	record = keyedMutationRecord{
		Version:     keyedMutationStateVersion,
		State:       keyedStateInProgress,
		Operation:   request.Operation,
		RequestHash: requestHash,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	if request.Operation == "job.start" {
		return d.executeNewKeyedJobStart(ctx, normalized, keyDigest, record)
	}
	if err := store.write(keyDigest, record); err != nil {
		return failedResult(request.Operation, "idempotency_state_failed", err.Error())
	}

	result := d.dispatchNative(ctx, normalized)
	record.State = keyedStateCompleted
	record.Result = &result
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.write(keyDigest, record); err != nil {
		return failedResult(request.Operation, "idempotency_state_failed", "native mutation completed but keyed completion state could not be published")
	}
	store.pruneBestEffort(keyDigest)
	return result
}

func (s *keyedMutationStore) activate(digest string) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if _, exists := s.active[digest]; exists {
		return false
	}
	s.active[digest] = struct{}{}
	return true
}

func (s *keyedMutationStore) deactivate(digest string) {
	s.activeMu.Lock()
	delete(s.active, digest)
	s.activeMu.Unlock()
}

func (s *keyedMutationStore) isActive(digest string) bool {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	_, active := s.active[digest]
	return active
}

func (s *keyedMutationStore) ensureRoot() error {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return errors.New("keyed mutation state directory is not configured")
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return fmt.Errorf("create keyed mutation state directory: %w", err)
	}
	if err := os.Chmod(s.root, 0700); err != nil {
		return fmt.Errorf("set keyed mutation state directory permissions: %w", err)
	}
	return nil
}

func (s *keyedMutationStore) statePath(digest string) string {
	return filepath.Join(s.root, digest+".json")
}

func (s *keyedMutationStore) lockPath(digest string) string {
	return filepath.Join(s.root, digest+".lock")
}

func (s *keyedMutationStore) lock(digest string) (*os.File, error) {
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(s.lockPath(digest), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open keyed mutation lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock keyed mutation state: %w", err)
	}
	return lock, nil
}

func unlockKeyedMutation(lock *os.File) {
	if lock == nil {
		return
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func (s *keyedMutationStore) read(digest string) (keyedMutationRecord, bool, error) {
	var record keyedMutationRecord
	payload, err := os.ReadFile(s.statePath(digest))
	if errors.Is(err, os.ErrNotExist) {
		return record, false, nil
	}
	if err != nil {
		return record, false, err
	}
	if len(payload) == 0 || len(payload) > keyedMutationMaxFileBytes {
		return record, false, errors.New("invalid keyed mutation state size")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, false, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return record, false, err
	}
	if err := validateKeyedMutationRecord(record); err != nil {
		return record, false, err
	}
	return record, true, nil
}

func validateKeyedMutationRecord(record keyedMutationRecord) error {
	if record.Version != keyedMutationStateVersion {
		return errors.New("unsupported keyed mutation state version")
	}
	if !isMutationOperation(record.Operation) {
		return errors.New("invalid keyed mutation operation")
	}
	if len(record.RequestHash) != sha256.Size*2 {
		return errors.New("invalid keyed mutation request hash")
	}
	if _, err := hex.DecodeString(record.RequestHash); err != nil {
		return errors.New("invalid keyed mutation request hash")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.UpdatedAt); err != nil {
		return errors.New("invalid keyed mutation update time")
	}
	switch record.State {
	case keyedStateInProgress:
		if record.Result != nil {
			return errors.New("in-progress keyed mutation has a result")
		}
	case keyedStateCompleted:
		if record.Result == nil {
			return errors.New("completed keyed mutation has no result")
		}
	default:
		return errors.New("invalid keyed mutation state")
	}
	return nil
}

func (s *keyedMutationStore) write(digest string, record keyedMutationRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode keyed mutation state: %w", err)
	}
	if len(payload) > keyedMutationMaxFileBytes {
		return errors.New("keyed mutation state exceeds bounded storage limit")
	}
	if err := writeFileDurable(s.statePath(digest), payload, 0600); err != nil {
		return fmt.Errorf("publish keyed mutation state: %w", err)
	}
	return nil
}

func writeFileDurable(path string, payload []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".hec-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := directoryHandle.Sync()
	closeErr := directoryHandle.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func removeFileDurable(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
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

func (s *keyedMutationStore) pruneBestEffort(current string) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return
	}
	type candidate struct {
		digest  string
		path    string
		updated time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	now := time.Now()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		digest := strings.TrimSuffix(name, ".json")
		if digest == current || s.isActive(digest) {
			continue
		}
		record, found, readErr := s.read(digest)
		if readErr != nil || !found || record.State != keyedStateCompleted {
			continue
		}
		updated, parseErr := time.Parse(time.RFC3339Nano, record.UpdatedAt)
		if parseErr != nil {
			continue
		}
		candidates = append(candidates, candidate{digest: digest, path: s.statePath(digest), updated: updated})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].updated.Before(candidates[j].updated) })
	removeCount := len(candidates) - keyedMutationMaxRecords
	for index, candidate := range candidates {
		if index >= removeCount && now.Sub(candidate.updated) <= keyedMutationRetention {
			continue
		}
		_ = os.Remove(candidate.path)
		_ = os.Remove(s.lockPath(candidate.digest))
	}
}

func digestString(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

var (
	callInputSchemaOnce sync.Once
	callInputSchema     *jsonschema.Resolved
	callInputSchemaErr  error
)

func resolvedCallInputSchema() (*jsonschema.Resolved, error) {
	callInputSchemaOnce.Do(func() {
		var schema jsonschema.Schema
		if err := json.Unmarshal(hecschemas.CallHECInput, &schema); err != nil {
			callInputSchemaErr = err
			return
		}
		callInputSchema, callInputSchemaErr = schema.Resolve(nil)
	})
	return callInputSchema, callInputSchemaErr
}

func validateCallRequestSchema(request CallRequest) error {
	resolved, err := resolvedCallInputSchema()
	if err != nil {
		return fmt.Errorf("resolve public input schema: %w", err)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode request for validation: %w", err)
	}
	var instance any
	if err := json.Unmarshal(payload, &instance); err != nil {
		return fmt.Errorf("decode request for validation: %w", err)
	}
	if err := resolved.Validate(instance); err != nil {
		return fmt.Errorf("request does not match the public operation schema: %w", err)
	}
	return nil
}

func normalizeMutationRequest(request CallRequest) (CallRequest, string, error) {
	if err := validateCallRequestSchema(request); err != nil {
		return CallRequest{}, "", err
	}
	var normalizedArgs map[string]any
	var err error
	switch request.Operation {
	case "run":
		normalizedArgs, err = normalizeTypedArgs[runArgs](request.Args, normalizeRunMutationArgs)
	case "job.start":
		normalizedArgs, err = normalizeTypedArgs[jobStartArgs](request.Args, normalizeJobStartMutationArgs)
	case "job.signal":
		normalizedArgs, err = normalizeTypedArgs[jobSignalArgs](request.Args, func(args *jobSignalArgs) error {
			if _, err := parseJobHandle(args.Handle); err != nil {
				return err
			}
			_, name, err := parseLinuxSignalName(args.Signal)
			args.Signal = name
			return err
		})
	case "job.forget":
		normalizedArgs, err = normalizeTypedArgs[jobHandleArgs](request.Args, func(args *jobHandleArgs) error {
			_, err := parseJobHandle(args.Handle)
			return err
		})
	case "terminal.open":
		normalizedArgs, err = normalizeTypedArgs[terminalOpenArgs](request.Args, func(args *terminalOpenArgs) error {
			if err := normalizeTerminalOpenArgs(args); err != nil {
				return err
			}
			canonicalizeEnvironment(&args.Env, &args.UnsetEnv)
			return nil
		})
	case "terminal.write":
		normalizedArgs, err = normalizeTypedArgs[terminalWriteArgs](request.Args, normalizeTerminalWriteMutationArgs)
	case "terminal.resize":
		normalizedArgs, err = normalizeTypedArgs[terminalResizeArgs](request.Args, func(args *terminalResizeArgs) error {
			if _, err := parseTerminalHandle(args.Handle); err != nil {
				return err
			}
			if err := validateTerminalDimension(args.Width, "width"); err != nil {
				return err
			}
			return validateTerminalDimension(args.Height, "height")
		})
	case "terminal.signal":
		normalizedArgs, err = normalizeTypedArgs[terminalSignalArgs](request.Args, func(args *terminalSignalArgs) error {
			if _, err := parseTerminalHandle(args.Handle); err != nil {
				return err
			}
			_, name, err := parseLinuxSignalName(args.Signal)
			args.Signal = name
			return err
		})
	case "terminal.close":
		normalizedArgs, err = normalizeTypedArgs[terminalHandleArgs](request.Args, func(args *terminalHandleArgs) error {
			_, err := parseTerminalHandle(args.Handle)
			return err
		})
	case "file.write", "file.append":
		normalizedArgs, err = normalizeTypedArgs[fileWriteArgs](request.Args, func(args *fileWriteArgs) error {
			content, err := decodeContent(args.Content, args.ContentBase64, request.Operation)
			if err != nil {
				return err
			}
			encoded := base64.StdEncoding.EncodeToString(content)
			args.Content = nil
			args.ContentBase64 = &encoded
			resolved, err := resolveFilesystemPath(args.Path)
			args.Path = resolved
			return err
		})
	case "file.patch":
		normalizedArgs, err = normalizeTypedArgs[filePatchArgs](request.Args, normalizeFilePatchMutationArgs)
	case "file.remove":
		normalizedArgs, err = normalizeTypedArgs[fileRemoveArgs](request.Args, func(args *fileRemoveArgs) error {
			resolved, err := resolveFilesystemPath(args.Path)
			args.Path = resolved
			return err
		})
	case "upload.begin":
		normalizedArgs, err = normalizeTypedArgs[uploadBeginArgs](request.Args, normalizeUploadBeginMutationArgs)
	case "upload.chunk":
		normalizedArgs, err = normalizeTypedArgs[uploadChunkArgs](request.Args, normalizeUploadChunkMutationArgs)
	case "upload.finish":
		normalizedArgs, err = normalizeTypedArgs[uploadFinishArgs](request.Args, normalizeUploadFinishMutationArgs)
	case "upload.abort":
		normalizedArgs, err = normalizeTypedArgs[uploadAbortArgs](request.Args, func(args *uploadAbortArgs) error {
			_, err := parseTypedHandle(args.Handle, "upload")
			return err
		})
	case "artifact.return":
		normalizedArgs, err = normalizeTypedArgs[artifactReturnArgs](request.Args, normalizeArtifactReturnMutationArgs)
	case "artifact.materialize":
		normalizedArgs, err = normalizeTypedArgs[artifactMaterializeArgs](request.Args, normalizeArtifactMaterializeMutationArgs)
	case "artifact.delete":
		normalizedArgs, err = normalizeTypedArgs[artifactHandleArgs](request.Args, func(args *artifactHandleArgs) error {
			_, err := parseTypedHandle(args.Handle, "artifact")
			return err
		})
	default:
		return CallRequest{}, "", fmt.Errorf("operation %q is not a keyed mutation", request.Operation)
	}
	if err != nil {
		return CallRequest{}, "", err
	}
	normalized := CallRequest{Operation: request.Operation, Args: normalizedArgs}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return CallRequest{}, "", err
	}
	return normalized, digestString(string(payload)), nil
}

func normalizeTypedArgs[T any](raw map[string]any, normalize func(*T) error) (map[string]any, error) {
	var value T
	if err := decodeOperationArgs(raw, &value); err != nil {
		return nil, err
	}
	if normalize != nil {
		if err := normalize(&value); err != nil {
			return nil, err
		}
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(payload, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeRunMutationArgs(args *runArgs) error {
	hasCommand := args.Command != nil
	hasArgv := args.Argv != nil
	if hasCommand == hasArgv {
		return errors.New("run requires exactly one of command or argv")
	}
	if hasArgv && (len(args.Argv) == 0 || args.Argv[0] == "") {
		return errors.New("argv must contain a nonempty executable name")
	}
	if args.Stdin != nil && args.StdinBase64 != nil {
		return errors.New("run accepts only one of stdin or stdin_base64")
	}
	stdin, err := decodeStdin(*args)
	if err != nil {
		return err
	}
	args.Stdin = nil
	args.StdinBase64 = nil
	if len(stdin) > 0 {
		encoded := base64.StdEncoding.EncodeToString(stdin)
		args.StdinBase64 = &encoded
	}
	canonicalizeEnvironment(&args.Env, &args.UnsetEnv)
	if _, err := buildEnvironment(args.Env, args.UnsetEnv); err != nil {
		return err
	}
	if _, err := directRunTimeout(args.TimeoutMS); err != nil {
		return errors.New("timeout_ms must be between 0 and 90000")
	}
	if args.MaxOutputBytes < 0 {
		return errors.New("max_output_bytes must be greater than or equal to zero")
	}
	if args.CWD == "" {
		args.CWD = DefaultCWD
	}
	if args.TimeoutMS == 0 {
		args.TimeoutMS = MaxDirectCall.Milliseconds()
	}
	if args.MaxOutputBytes == 0 {
		args.MaxOutputBytes = DefaultMaxOutputBytes
	}
	return nil
}

func normalizeJobStartMutationArgs(args *jobStartArgs) error {
	if err := validateJobStartArgs(*args); err != nil {
		return err
	}
	stdin, _, err := decodeJobStdin(*args)
	if err != nil {
		return err
	}
	args.Stdin = nil
	args.StdinBase64 = nil
	if len(stdin) > 0 {
		encoded := base64.StdEncoding.EncodeToString(stdin)
		args.StdinBase64 = &encoded
	}
	canonicalizeEnvironment(&args.Env, &args.UnsetEnv)
	if _, err := durationFromMilliseconds(args.TimeoutMS); err != nil {
		return errors.New("timeout_ms is out of range")
	}
	if args.CWD == "" {
		args.CWD = DefaultCWD
	}
	return nil
}

func normalizeTerminalWriteMutationArgs(args *terminalWriteArgs) error {
	if _, err := parseTerminalHandle(args.Handle); err != nil {
		return err
	}
	if (args.Data == nil) == (args.DataBase64 == nil) {
		return errors.New("terminal.write requires exactly one of data or data_base64")
	}
	var data []byte
	if args.Data != nil {
		data = []byte(*args.Data)
	} else {
		decoded, err := decodeChunk(*args.DataBase64)
		if err != nil {
			return err
		}
		data = decoded
	}
	if int64(len(data)) > MaximumTerminalWriteBytes {
		return errors.New("terminal.write data exceeds 1048576 bytes")
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	args.Data = nil
	args.DataBase64 = &encoded
	return nil
}

func normalizeFilePatchMutationArgs(args *filePatchArgs) error {
	if args.Patch == "" {
		return errors.New("patch is required")
	}
	if args.CWD == "" {
		args.CWD = DefaultCWD
	}
	resolved, err := resolveFilesystemPath(args.CWD)
	if err != nil {
		return err
	}
	args.CWD = resolved
	strip := 1
	if args.Strip != nil {
		strip = *args.Strip
	}
	if strip < 0 || strip > 16 {
		return errors.New("strip must be between 0 and 16")
	}
	args.Strip = &strip
	return nil
}

func normalizeUploadBeginMutationArgs(args *uploadBeginArgs) error {
	if err := validateBasename(args.Filename, "filename"); err != nil {
		return err
	}
	if args.Size != nil && *args.Size < 0 {
		return errors.New("size must be greater than or equal to zero")
	}
	return validateSHA256(args.SHA256)
}

func normalizeUploadChunkMutationArgs(args *uploadChunkArgs) error {
	if _, err := parseTypedHandle(args.Handle, "upload"); err != nil {
		return err
	}
	if args.Offset == nil || *args.Offset < 0 {
		return errors.New("offset must be present and greater than or equal to zero")
	}
	if args.DataBase64 == nil {
		return errors.New("data_base64 is required")
	}
	decoded, err := decodeChunk(*args.DataBase64)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(decoded)
	args.DataBase64 = &encoded
	return nil
}

func normalizeUploadFinishMutationArgs(args *uploadFinishArgs) error {
	if _, err := parseTypedHandle(args.Handle, "upload"); err != nil {
		return err
	}
	artifactTarget := args.Artifact != nil && *args.Artifact
	if args.Artifact != nil && !*args.Artifact {
		return errors.New("artifact must be true when provided")
	}
	if (args.Destination != nil) == artifactTarget {
		return errors.New("upload.finish requires exactly one of destination or artifact true")
	}
	if args.Destination != nil {
		if !filepath.IsAbs(*args.Destination) {
			return errors.New("destination must be absolute")
		}
		clean := filepath.Clean(*args.Destination)
		args.Destination = &clean
		if args.Name != "" || args.MediaType != "" {
			return errors.New("name and media_type apply only to artifact completion")
		}
	} else if args.Overwrite {
		return errors.New("overwrite applies only to destination completion")
	}
	if args.Name != "" {
		return validateBasename(args.Name, "name")
	}
	return nil
}

func normalizeArtifactReturnMutationArgs(args *artifactReturnArgs) error {
	resolved, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return err
	}
	args.Path = resolved
	if args.Name != "" {
		return validateBasename(args.Name, "name")
	}
	return nil
}

func normalizeArtifactMaterializeMutationArgs(args *artifactMaterializeArgs) error {
	if _, err := parseTypedHandle(args.Handle, "artifact"); err != nil {
		return err
	}
	if !filepath.IsAbs(args.Destination) {
		return errors.New("destination must be absolute")
	}
	args.Destination = filepath.Clean(args.Destination)
	return nil
}

func canonicalizeEnvironment(environment *map[string]string, unset *[]string) {
	if environment != nil && len(*environment) == 0 {
		*environment = nil
	}
	if unset == nil || len(*unset) == 0 {
		if unset != nil {
			*unset = nil
		}
		return
	}
	ordered := append([]string(nil), (*unset)...)
	sort.Strings(ordered)
	unique := ordered[:0]
	for _, value := range ordered {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	*unset = unique
}

func rehydrateStoredResult(result Result) Result {
	resources := make([]any, 0, len(result.Resources))
	for _, resource := range result.Resources {
		if descriptor, ok := resource.(ResourceDescriptor); ok {
			resources = append(resources, descriptor)
			continue
		}
		payload, err := json.Marshal(resource)
		if err != nil {
			resources = append(resources, resource)
			continue
		}
		var descriptor ResourceDescriptor
		if err := json.Unmarshal(payload, &descriptor); err == nil && descriptor.URI != "" {
			resources = append(resources, descriptor)
		} else {
			resources = append(resources, resource)
		}
	}
	result.Resources = resources
	if result.Result == nil {
		result.Result = map[string]any{}
	}
	return result
}
