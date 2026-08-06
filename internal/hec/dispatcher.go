package hec

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type Dispatcher struct {
	jobsDir         string
	jobKeysDir      string
	mutationKeysDir string
	keyedState      *keyedMutationStore
	hecBinaryPath   string
	systemdRunPath  string
	systemctlPath   string
	uploadsDir      string
	artifactsDir    string
	terminalsDir    string
	tmuxPath        string
	tmuxSocket      string
	tmuxScopeUnit   string
	infocmpPath     string
	gitPath         string
	patchPath       string
	tarPath         string
	zstdPath        string
	capabilityDir   string
	skillRoots      []skillRoot
	workspaceRoot   string
	recipeDir       string
	commandPath     string
	operationSchema []byte
}

func NewDispatcher() *Dispatcher {
	dispatcher := &Dispatcher{
		jobsDir:         JobRootDir,
		jobKeysDir:      JobKeyDir,
		mutationKeysDir: MutationKeyRootDir,
		hecBinaryPath:   JobBinaryPath,
		systemdRunPath:  "/usr/bin/systemd-run",
		systemctlPath:   "/usr/bin/systemctl",
		uploadsDir:      UploadRootDir,
		artifactsDir:    ArtifactRootDir,
		terminalsDir:    TerminalRootDir,
		tmuxPath:        "/usr/bin/tmux",
		tmuxSocket:      TerminalTmuxSocket,
		tmuxScopeUnit:   "hec-tmux",
		infocmpPath:     "/usr/bin/infocmp",
		gitPath:         "/usr/bin/git",
		patchPath:       "/usr/bin/patch",
		tarPath:         "/usr/bin/tar",
		zstdPath:        "/usr/bin/zstd",
		capabilityDir:   DefaultCapabilityDir,
		skillRoots: []skillRoot{
			{Path: DefaultBuiltinSkillRoot, Source: "builtin"},
			{Path: DefaultOwnerSkillRoot, Source: "owner"},
		},
		workspaceRoot: DefaultWorkspaceRoot,
		recipeDir:     DefaultRecipeDir,
		commandPath:   os.Getenv("PATH"),
	}
	dispatcher.keyedState = newKeyedMutationStore(dispatcher.mutationKeysDir)
	return dispatcher
}

func (d *Dispatcher) Dispatch(ctx context.Context, request CallRequest) Result {
	operation := strings.TrimSpace(request.Operation)
	if operation == "" {
		return failedResult("", "invalid_operation", "operation must be a nonempty string")
	}
	request.Operation = operation
	if request.Args == nil {
		request.Args = map[string]any{}
	}
	if request.IdempotencyKey != "" && isMutationOperation(operation) {
		return d.dispatchKeyedMutation(ctx, request)
	}
	return d.dispatchNative(ctx, request)
}

func (d *Dispatcher) dispatchNative(ctx context.Context, request CallRequest) Result {
	operation := request.Operation
	switch operation {
	case "health":
		if len(request.Args) != 0 {
			return failedResult(operation, "invalid_arguments", "health does not accept arguments")
		}
		result := newResult(operation)
		result.OK = true
		result.Result = map[string]any{"alive": true}
		return result
	case "version":
		if len(request.Args) != 0 {
			return failedResult(operation, "invalid_arguments", "version does not accept arguments")
		}
		result := newResult(operation)
		result.OK = true
		result.Result = map[string]any{
			"version":  Version,
			"protocol": ProtocolVersion,
			"build": map[string]any{
				"commit": BuildCommit,
				"date":   BuildDate,
			},
		}
		return result
	case "run":
		return d.run(ctx, request.Args)
	case "capabilities":
		return d.capabilities(ctx, request.Args)
	case "skill.list":
		return d.skillList(ctx, request.Args)
	case "skill.find":
		return d.skillFind(ctx, request.Args)
	case "skill.read":
		return d.skillRead(ctx, request.Args)
	case "job.start":
		return d.jobStart(ctx, request.Args, "")
	case "job.status":
		return d.jobStatus(ctx, request.Args)
	case "job.output":
		return d.jobOutput(ctx, request.Args)
	case "job.wait":
		return d.jobWait(ctx, request.Args)
	case "job.signal":
		return d.jobSignal(ctx, request.Args)
	case "job.list":
		return d.jobList(ctx, request.Args)
	case "job.forget":
		return d.jobForget(ctx, request.Args)
	case "terminal.open":
		return d.terminalOpen(ctx, request.Args)
	case "terminal.list":
		return d.terminalList(ctx, request.Args)
	case "terminal.read":
		return d.terminalRead(ctx, request.Args)
	case "terminal.write":
		return d.terminalWrite(ctx, request.Args)
	case "terminal.resize":
		return d.terminalResize(ctx, request.Args)
	case "terminal.signal":
		return d.terminalSignal(ctx, request.Args)
	case "terminal.close":
		return d.terminalClose(ctx, request.Args)
	case "file.stat":
		return d.fileStat(ctx, request.Args)
	case "file.list":
		return d.fileList(ctx, request.Args)
	case "file.read":
		return d.fileRead(ctx, request.Args)
	case "file.write":
		return d.fileWrite(ctx, request.Args)
	case "file.append":
		return d.fileAppend(ctx, request.Args)
	case "file.patch":
		return d.filePatch(ctx, request.Args)
	case "file.remove":
		return d.fileRemove(ctx, request.Args)
	case "upload.begin":
		return d.uploadBegin(ctx, request.Args)
	case "upload.chunk":
		return d.uploadChunk(ctx, request.Args)
	case "upload.finish":
		return d.uploadFinish(ctx, request.Args)
	case "upload.abort":
		return d.uploadAbort(ctx, request.Args)
	case "artifact.return":
		return d.artifactReturn(ctx, request.Args)
	case "artifact.stat":
		return d.artifactStat(ctx, request.Args)
	case "artifact.read":
		return d.artifactRead(ctx, request.Args)
	case "artifact.materialize":
		return d.artifactMaterialize(ctx, request.Args)
	case "artifact.list":
		return d.artifactList(ctx, request.Args)
	case "artifact.delete":
		return d.artifactDelete(ctx, request.Args)
	default:
		return failedResult(operation, "operation_not_found", fmt.Sprintf("unknown operation %q", operation))
	}
}
