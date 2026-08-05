package hec

import (
	"context"
	"fmt"
	"strings"
)

type Dispatcher struct {
	jobsDir        string
	jobKeysDir     string
	hecBinaryPath  string
	systemdRunPath string
	systemctlPath  string
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		jobsDir:        JobRootDir,
		jobKeysDir:     JobKeyDir,
		hecBinaryPath:  JobBinaryPath,
		systemdRunPath: "/usr/bin/systemd-run",
		systemctlPath:  "/usr/bin/systemctl",
	}
}

func (d *Dispatcher) Dispatch(ctx context.Context, request CallRequest) Result {
	operation := strings.TrimSpace(request.Operation)
	if operation == "" {
		return failedResult("", "invalid_operation", "operation must be a nonempty string")
	}

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
	case "job.start":
		return d.jobStart(ctx, request.Args, request.IdempotencyKey)
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
	default:
		return failedResult(operation, "operation_not_found", fmt.Sprintf("unknown operation %q", operation))
	}
}
