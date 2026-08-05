package hec

import "fmt"

const ProtocolVersion = "HEC1/1.0.0"

const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
	StatusCancelled = "cancelled"
)

type CallRequest struct {
	Operation      string         `json:"operation"`
	Args           map[string]any `json:"args,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	OK             bool           `json:"ok"`
	Protocol       string         `json:"protocol"`
	Operation      string         `json:"operation"`
	Status         string         `json:"status"`
	Handle         *string        `json:"handle"`
	ExitCode       *int           `json:"exit_code"`
	Signal         *string        `json:"signal"`
	Stdout         string         `json:"stdout"`
	Stderr         string         `json:"stderr"`
	StdoutEncoding string         `json:"stdout_encoding"`
	StderrEncoding string         `json:"stderr_encoding"`
	Truncated      bool           `json:"truncated"`
	Result         map[string]any `json:"result"`
	Resources      []any          `json:"resources"`
	Error          *ErrorDetail   `json:"error"`
}

func newResult(operation string) Result {
	return Result{
		Protocol:       ProtocolVersion,
		Operation:      operation,
		Status:         StatusCompleted,
		StdoutEncoding: "utf8",
		StderrEncoding: "utf8",
		Result:         map[string]any{},
		Resources:      []any{},
	}
}

func failedResult(operation, code, message string) Result {
	result := newResult(operation)
	result.Status = StatusFailed
	result.Error = &ErrorDetail{Code: code, Message: message}
	return result
}

func (r Result) Summary() string {
	switch r.Operation {
	case "health":
		if r.OK {
			return "HEC is alive."
		}
	case "version":
		if r.OK {
			return fmt.Sprintf("HEC %s (%s).", Version, ProtocolVersion)
		}
	case "run":
		if r.OK && r.ExitCode != nil {
			return fmt.Sprintf("Completed run with exit code %d.", *r.ExitCode)
		}
		if r.Status == StatusTimedOut {
			return "Run timed out."
		}
		if r.Status == StatusCancelled {
			return "Run was cancelled."
		}
		if r.Signal != nil {
			return fmt.Sprintf("Run terminated by %s.", *r.Signal)
		}
		if r.ExitCode != nil {
			return fmt.Sprintf("Run failed with exit code %d.", *r.ExitCode)
		}
	}
	if r.Error != nil {
		return r.Error.Message
	}
	if r.OK {
		return fmt.Sprintf("Completed %s.", r.Operation)
	}
	return fmt.Sprintf("%s failed.", r.Operation)
}
