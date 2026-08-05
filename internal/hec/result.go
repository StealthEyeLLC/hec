package hec

import (
	"fmt"
	"reflect"
)

const ProtocolVersion = "HEC1/1.0.0"

const (
	StatusStarting  = "starting"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusTimedOut  = "timed_out"
	StatusCancelled = "cancelled"
	StatusUnknown   = "unknown"
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
	case "capabilities":
		if r.OK {
			if count, ok := resultInt64(r.Result["count"]); ok {
				return fmt.Sprintf("Found %d capabilities.", count)
			}
		}
	case "skill.list":
		if r.OK {
			return fmt.Sprintf("Listed %d skills.", resultLength(r.Result["skills"]))
		}
	case "skill.find":
		if r.OK {
			if count, ok := resultInt64(r.Result["count"]); ok {
				return fmt.Sprintf("Found %d skills.", count)
			}
		}
	case "skill.read":
		if r.OK {
			return fmt.Sprintf("Read skill %s.", resultString(r.Result, "name"))
		}
	case "job.start":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Started %s.", *r.Handle)
		}
	case "job.status":
		if r.OK {
			if handle, ok := r.Result["handle"].(string); ok {
				if status, ok := r.Result["job_status"].(string); ok {
					return fmt.Sprintf("%s is %s.", handle, status)
				}
			}
		}
	case "job.output":
		if r.OK && r.Handle != nil {
			offset, _ := resultInt64(r.Result["offset"])
			next, _ := resultInt64(r.Result["next_offset"])
			stream, _ := r.Result["stream"].(string)
			return fmt.Sprintf("Read %d bytes from %s %s.", next-offset, *r.Handle, stream)
		}
	case "job.wait":
		if r.OK && r.Handle != nil {
			status, _ := r.Result["job_status"].(string)
			if waitTimedOut, _ := r.Result["wait_timed_out"].(bool); waitTimedOut {
				return fmt.Sprintf("%s is still %s after the wait timeout.", *r.Handle, status)
			}
			if exitCode, ok := resultInt64(r.Result["exit_code"]); ok {
				return fmt.Sprintf("%s %s with exit code %d.", *r.Handle, status, exitCode)
			}
			if signalName, ok := r.Result["signal"].(string); ok {
				return fmt.Sprintf("%s %s by %s.", *r.Handle, status, signalName)
			}
			return fmt.Sprintf("%s is %s.", *r.Handle, status)
		}
	case "job.signal":
		if r.OK && r.Handle != nil {
			if signalName, ok := r.Result["signal"].(string); ok {
				return fmt.Sprintf("Sent %s to %s.", signalName, *r.Handle)
			}
		}
	case "job.list":
		if r.OK {
			if count, ok := resultInt64(r.Result["count"]); ok {
				return fmt.Sprintf("Listed %d jobs.", count)
			}
		}
	case "job.forget":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Forgot %s.", *r.Handle)
		}
	case "terminal.open":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Opened %s.", *r.Handle)
		}
	case "terminal.list":
		if r.OK {
			if count, ok := resultInt64(r.Result["count"]); ok {
				return fmt.Sprintf("Listed %d terminals.", count)
			}
		}
	case "terminal.read":
		if r.OK && r.Handle != nil {
			if resultString(r.Result, "mode") == "screen" {
				return fmt.Sprintf("Read the current screen from %s.", *r.Handle)
			}
			return fmt.Sprintf("Read %d bytes from %s output.", resultRangeLength(r.Result), *r.Handle)
		}
	case "terminal.write":
		if r.OK && r.Handle != nil {
			count, _ := resultInt64(r.Result["bytes_written"])
			return fmt.Sprintf("Wrote %d bytes to %s.", count, *r.Handle)
		}
	case "terminal.resize":
		if r.OK && r.Handle != nil {
			width, _ := resultInt64(r.Result["width"])
			height, _ := resultInt64(r.Result["height"])
			return fmt.Sprintf("Resized %s to %dx%d.", *r.Handle, width, height)
		}
	case "terminal.signal":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Sent %s to %s.", resultString(r.Result, "signal"), *r.Handle)
		}
	case "terminal.close":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Closed %s.", *r.Handle)
		}
	case "file.stat":
		if r.OK {
			return fmt.Sprintf("Read metadata for %s.", resultString(r.Result, "path"))
		}
	case "file.list":
		if r.OK {
			return fmt.Sprintf("Listed %d entries in %s.", resultLength(r.Result["entries"]), resultString(r.Result, "path"))
		}
	case "file.read":
		if r.OK {
			return fmt.Sprintf("Read %d bytes from %s.", resultRangeLength(r.Result), resultString(r.Result, "path"))
		}
	case "file.write":
		if r.OK {
			count, _ := resultInt64(r.Result["bytes_written"])
			return fmt.Sprintf("Wrote %d bytes to %s.", count, resultString(r.Result, "path"))
		}
	case "file.append":
		if r.OK {
			count, _ := resultInt64(r.Result["bytes_appended"])
			return fmt.Sprintf("Appended %d bytes to %s.", count, resultString(r.Result, "path"))
		}
	case "file.patch":
		if r.OK {
			return fmt.Sprintf("Applied patch in %s.", resultString(r.Result, "cwd"))
		}
	case "file.remove":
		if r.OK {
			return fmt.Sprintf("Removed %s.", resultString(r.Result, "path"))
		}
	case "upload.begin":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Started %s.", *r.Handle)
		}
	case "upload.chunk":
		if r.OK && r.Handle != nil {
			count, _ := resultInt64(r.Result["bytes_written"])
			return fmt.Sprintf("Wrote %d bytes to %s.", count, *r.Handle)
		}
	case "upload.finish":
		if r.OK {
			return fmt.Sprintf("Completed %s.", resultString(r.Result, "upload_handle"))
		}
	case "upload.abort":
		if r.OK {
			return fmt.Sprintf("Aborted %s.", resultString(r.Result, "handle"))
		}
	case "artifact.return":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Returned %s.", *r.Handle)
		}
	case "artifact.stat":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Read metadata for %s.", *r.Handle)
		}
	case "artifact.read":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Read %d bytes from %s.", resultRangeLength(r.Result), *r.Handle)
		}
	case "artifact.materialize":
		if r.OK && r.Handle != nil {
			return fmt.Sprintf("Materialized %s to %s.", *r.Handle, resultString(r.Result, "destination"))
		}
	case "artifact.list":
		if r.OK {
			return fmt.Sprintf("Listed %d artifacts.", resultLength(r.Result["artifacts"]))
		}
	case "artifact.delete":
		if r.OK {
			return fmt.Sprintf("Deleted %s.", resultString(r.Result, "handle"))
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

func resultString(result map[string]any, key string) string {
	value, _ := result[key].(string)
	return value
}

func resultRangeLength(result map[string]any) int64 {
	offset, _ := resultInt64(result["offset"])
	next, _ := resultInt64(result["next_offset"])
	return next - offset
}

func resultLength(value any) int {
	if value == nil {
		return 0
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array {
		return reflected.Len()
	}
	return 0
}
