package hec

import (
	"context"
	"fmt"
	"strings"
)

type Dispatcher struct{}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{}
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
	default:
		return failedResult(operation, "operation_not_found", fmt.Sprintf("unknown operation %q", operation))
	}
}
