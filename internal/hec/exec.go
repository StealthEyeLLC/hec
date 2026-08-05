package hec

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	DefaultCWD            = "/root"
	DefaultMaxOutputBytes = int64(1 << 20)
)

type runArgs struct {
	Command        *string           `json:"command"`
	Argv           []string          `json:"argv"`
	CWD            string            `json:"cwd"`
	Env            map[string]string `json:"env"`
	UnsetEnv       []string          `json:"unset_env"`
	Stdin          *string           `json:"stdin"`
	StdinBase64    *string           `json:"stdin_base64"`
	TimeoutMS      int64             `json:"timeout_ms"`
	MaxOutputBytes int64             `json:"max_output_bytes"`
}

func (d *Dispatcher) run(ctx context.Context, raw map[string]any) Result {
	var args runArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("run", "invalid_arguments", err.Error())
	}

	hasCommand := args.Command != nil
	hasArgv := args.Argv != nil
	if hasCommand == hasArgv {
		return failedResult("run", "invalid_arguments", "run requires exactly one of command or argv")
	}
	if hasArgv && (len(args.Argv) == 0 || args.Argv[0] == "") {
		return failedResult("run", "invalid_arguments", "argv must contain a nonempty executable name")
	}
	if args.Stdin != nil && args.StdinBase64 != nil {
		return failedResult("run", "invalid_arguments", "run accepts only one of stdin or stdin_base64")
	}
	if args.TimeoutMS < 0 {
		return failedResult("run", "invalid_arguments", "timeout_ms must be greater than or equal to zero")
	}
	if args.MaxOutputBytes < 0 {
		return failedResult("run", "invalid_arguments", "max_output_bytes must be greater than or equal to zero")
	}

	stdin, err := decodeStdin(args)
	if err != nil {
		return failedResult("run", "invalid_arguments", err.Error())
	}
	environment, err := buildEnvironment(args.Env, args.UnsetEnv)
	if err != nil {
		return failedResult("run", "invalid_arguments", err.Error())
	}

	maxOutput := args.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = DefaultMaxOutputBytes
	}
	limiter := newOutputLimiter(maxOutput)
	stdout := limiter.writer()
	stderr := limiter.writer()

	execCtx := ctx
	cancel := func() {}
	if args.TimeoutMS > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(args.TimeoutMS)*time.Millisecond)
	}
	defer cancel()

	var command *exec.Cmd
	if hasCommand {
		command = exec.CommandContext(execCtx, "/bin/bash", "-lc", *args.Command)
	} else {
		command = exec.CommandContext(execCtx, args.Argv[0], args.Argv[1:]...)
	}
	command.Dir = args.CWD
	if command.Dir == "" {
		command.Dir = DefaultCWD
	}
	command.Env = environment
	command.Stdin = bytes.NewReader(stdin)
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}

	runErr := command.Run()
	stdoutValue, stdoutEncoding := encodeOutput(stdout.bytes())
	stderrValue, stderrEncoding := encodeOutput(stderr.bytes())

	result := newResult("run")
	result.Stdout = stdoutValue
	result.Stderr = stderrValue
	result.StdoutEncoding = stdoutEncoding
	result.StderrEncoding = stderrEncoding
	result.Truncated = limiter.truncated()

	if command.ProcessState != nil {
		setProcessOutcome(&result, command.ProcessState)
	}

	switch {
	case runErr == nil:
		result.OK = true
		result.Status = StatusCompleted
		if result.ExitCode == nil {
			exitCode := 0
			result.ExitCode = &exitCode
		}
		return result
	case errors.Is(execCtx.Err(), context.DeadlineExceeded):
		result.Status = StatusTimedOut
		result.Error = &ErrorDetail{
			Code:    "timeout",
			Message: fmt.Sprintf("process timed out after %d ms", args.TimeoutMS),
		}
		return result
	case errors.Is(execCtx.Err(), context.Canceled):
		result.Status = StatusCancelled
		result.Error = &ErrorDetail{Code: "cancelled", Message: "process was cancelled"}
		return result
	case command.ProcessState == nil:
		result.Status = StatusFailed
		result.Error = &ErrorDetail{Code: "execution_failed", Message: runErr.Error()}
		return result
	default:
		result.Status = StatusFailed
		if result.Signal != nil {
			result.Error = &ErrorDetail{
				Code:    "process_signaled",
				Message: fmt.Sprintf("process terminated by %s", *result.Signal),
			}
		} else if result.ExitCode != nil {
			result.Error = &ErrorDetail{
				Code:    "process_failed",
				Message: fmt.Sprintf("process exited with code %d", *result.ExitCode),
			}
		} else {
			result.Error = &ErrorDetail{Code: "process_failed", Message: runErr.Error()}
		}
		return result
	}
}

func decodeOperationArgs(raw map[string]any, destination any) error {
	if raw == nil {
		raw = map[string]any{}
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encode operation arguments: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode operation arguments: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode operation arguments: %w", err)
	}
	return errors.New("decode operation arguments: trailing JSON value")
}

func decodeStdin(args runArgs) ([]byte, error) {
	if args.Stdin != nil {
		return []byte(*args.Stdin), nil
	}
	if args.StdinBase64 == nil {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(*args.StdinBase64)
	if err != nil {
		return nil, fmt.Errorf("stdin_base64 is not valid base64: %w", err)
	}
	return decoded, nil
}

func buildEnvironment(additions map[string]string, removals []string) ([]string, error) {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			values[name] = value
		}
	}
	for _, name := range removals {
		if err := validateEnvironmentName(name); err != nil {
			return nil, fmt.Errorf("unset_env: %w", err)
		}
		delete(values, name)
	}
	for name, value := range additions {
		if err := validateEnvironmentName(name); err != nil {
			return nil, fmt.Errorf("env: %w", err)
		}
		if strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("env value for %q contains NUL", name)
		}
		values[name] = value
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	environment := make([]string, 0, len(names))
	for _, name := range names {
		environment = append(environment, name+"="+values[name])
	}
	return environment, nil
}

func validateEnvironmentName(name string) error {
	if name == "" {
		return errors.New("environment variable name must not be empty")
	}
	if strings.ContainsAny(name, "=\x00") {
		return fmt.Errorf("invalid environment variable name %q", name)
	}
	return nil
}

func setProcessOutcome(result *Result, state *os.ProcessState) {
	waitStatus, ok := state.Sys().(syscall.WaitStatus)
	if !ok {
		exitCode := state.ExitCode()
		if exitCode >= 0 {
			result.ExitCode = &exitCode
		}
		return
	}
	if waitStatus.Signaled() {
		signal := linuxSignalName(waitStatus.Signal())
		result.Signal = &signal
		return
	}
	exitCode := waitStatus.ExitStatus()
	result.ExitCode = &exitCode
}

func linuxSignalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGHUP:
		return "SIGHUP"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGILL:
		return "SIGILL"
	case syscall.SIGTRAP:
		return "SIGTRAP"
	case syscall.SIGABRT:
		return "SIGABRT"
	case syscall.SIGBUS:
		return "SIGBUS"
	case syscall.SIGFPE:
		return "SIGFPE"
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGUSR1:
		return "SIGUSR1"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	case syscall.SIGUSR2:
		return "SIGUSR2"
	case syscall.SIGPIPE:
		return "SIGPIPE"
	case syscall.SIGALRM:
		return "SIGALRM"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGCHLD:
		return "SIGCHLD"
	case syscall.SIGCONT:
		return "SIGCONT"
	case syscall.SIGSTOP:
		return "SIGSTOP"
	case syscall.SIGTSTP:
		return "SIGTSTP"
	case syscall.SIGTTIN:
		return "SIGTTIN"
	case syscall.SIGTTOU:
		return "SIGTTOU"
	case syscall.SIGURG:
		return "SIGURG"
	case syscall.SIGXCPU:
		return "SIGXCPU"
	case syscall.SIGXFSZ:
		return "SIGXFSZ"
	case syscall.SIGVTALRM:
		return "SIGVTALRM"
	case syscall.SIGPROF:
		return "SIGPROF"
	case syscall.SIGWINCH:
		return "SIGWINCH"
	case syscall.SIGIO:
		return "SIGIO"
	case syscall.SIGSYS:
		return "SIGSYS"
	default:
		return "SIG" + strconv.Itoa(int(signal))
	}
}

func encodeOutput(data []byte) (string, string) {
	if utf8.Valid(data) {
		return string(data), "utf8"
	}
	return base64.StdEncoding.EncodeToString(data), "base64"
}

type outputLimiter struct {
	mu            sync.Mutex
	remaining     int64
	truncatedFlag bool
}

type limitedOutputWriter struct {
	limiter *outputLimiter
	buffer  bytes.Buffer
}

func newOutputLimiter(limit int64) *outputLimiter {
	return &outputLimiter{remaining: limit}
}

func (l *outputLimiter) writer() *limitedOutputWriter {
	return &limitedOutputWriter{limiter: l}
}

func (w *limitedOutputWriter) Write(data []byte) (int, error) {
	w.limiter.mu.Lock()
	defer w.limiter.mu.Unlock()

	kept := int64(len(data))
	if kept > w.limiter.remaining {
		kept = w.limiter.remaining
		w.limiter.truncatedFlag = true
	}
	if kept > 0 {
		_, _ = w.buffer.Write(data[:int(kept)])
		w.limiter.remaining -= kept
	}
	if kept < int64(len(data)) {
		w.limiter.truncatedFlag = true
	}
	return len(data), nil
}

func (w *limitedOutputWriter) bytes() []byte {
	w.limiter.mu.Lock()
	defer w.limiter.mu.Unlock()
	return bytes.Clone(w.buffer.Bytes())
}

func (l *outputLimiter) truncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.truncatedFlag
}
