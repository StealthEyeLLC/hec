package hec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

func RunJob(specPath string) int {
	resultPath := filepath.Join(filepath.Dir(specPath), "result.json")
	spec, err := decodeJobSpec(specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hec job-run:", err)
		result := jobFinalResult{Status: JobStatusFailed, Complete: true}
		exitCode := 125
		result.ExitCode = &exitCode
		if writeErr := writeJSONAtomic(resultPath, result, 0600); writeErr != nil {
			fmt.Fprintln(os.Stderr, "hec job-run: write result:", writeErr)
		}
		return exitCode
	}
	environment, err := buildEnvironment(spec.Env, spec.UnsetEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hec job-run:", err)
		result := jobFinalResult{Status: JobStatusFailed, Complete: true}
		exitCode := 125
		result.ExitCode = &exitCode
		_ = writeJSONAtomic(resultPath, result, 0600)
		return exitCode
	}

	var command *exec.Cmd
	if spec.Command != nil {
		command = exec.Command("/bin/bash", "-lc", *spec.Command)
	} else {
		command = exec.Command(spec.Argv[0], spec.Argv[1:]...)
	}
	command.Dir = spec.CWD
	command.Env = environment
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if spec.StdinPath != "" {
		stdin, openErr := os.Open(spec.StdinPath)
		if openErr != nil {
			fmt.Fprintln(os.Stderr, "hec job-run: open stdin:", openErr)
			result := jobFinalResult{Status: JobStatusFailed, Complete: true}
			exitCode := 125
			result.ExitCode = &exitCode
			_ = writeJSONAtomic(resultPath, result, 0600)
			return exitCode
		}
		defer stdin.Close()
		command.Stdin = stdin
	} else {
		stdin, openErr := os.Open(os.DevNull)
		if openErr != nil {
			fmt.Fprintln(os.Stderr, "hec job-run: open /dev/null:", openErr)
			exitCode := 125
			result := jobFinalResult{Status: JobStatusFailed, ExitCode: &exitCode, Complete: true}
			_ = writeJSONAtomic(resultPath, result, 0600)
			return exitCode
		}
		defer stdin.Close()
		command.Stdin = stdin
	}

	if err := command.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "hec job-run: start:", err)
		result := jobFinalResult{Status: JobStatusFailed, Complete: true}
		exitCode := 127
		result.ExitCode = &exitCode
		if writeErr := writeJSONAtomic(resultPath, result, 0600); writeErr != nil {
			fmt.Fprintln(os.Stderr, "hec job-run: write result:", writeErr)
			return 125
		}
		return exitCode
	}

	var timedOut atomic.Bool
	stopForwarding := make(chan struct{})
	signalChannel := make(chan os.Signal, 16)
	forwardedSignals := []os.Signal{
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGILL,
		syscall.SIGTRAP,
		syscall.SIGABRT,
		syscall.SIGBUS,
		syscall.SIGFPE,
		syscall.SIGUSR1,
		syscall.SIGSEGV,
		syscall.SIGUSR2,
		syscall.SIGPIPE,
		syscall.SIGALRM,
		syscall.SIGTERM,
		syscall.SIGCONT,
		syscall.SIGTSTP,
		syscall.SIGTTIN,
		syscall.SIGTTOU,
		syscall.SIGXCPU,
		syscall.SIGXFSZ,
		syscall.SIGVTALRM,
		syscall.SIGPROF,
		syscall.SIGWINCH,
		syscall.SIGIO,
		syscall.SIGSYS,
	}
	signal.Notify(signalChannel, forwardedSignals...)
	defer signal.Stop(signalChannel)
	go func() {
		for {
			select {
			case received := <-signalChannel:
				linuxSignal, ok := received.(syscall.Signal)
				if !ok {
					continue
				}
				_ = syscall.Kill(-command.Process.Pid, linuxSignal)
			case <-stopForwarding:
				return
			}
		}
	}()

	var timeout *time.Timer
	if spec.TimeoutMS > 0 {
		timeout = time.AfterFunc(time.Duration(spec.TimeoutMS)*time.Millisecond, func() {
			timedOut.Store(true)
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		})
	}

	waitErr := command.Wait()
	if timeout != nil {
		timeout.Stop()
	}
	close(stopForwarding)

	result := jobFinalResult{Complete: true, TimedOut: timedOut.Load()}
	var mirrorSignal syscall.Signal
	if command.ProcessState != nil {
		waitStatus, ok := command.ProcessState.Sys().(syscall.WaitStatus)
		if ok && waitStatus.Signaled() {
			name := linuxSignalName(waitStatus.Signal())
			result.Signal = &name
			mirrorSignal = waitStatus.Signal()
			if result.TimedOut {
				result.Status = JobStatusTimedOut
			} else {
				result.Status = JobStatusCancelled
			}
		} else {
			exitCode := command.ProcessState.ExitCode()
			result.ExitCode = &exitCode
			if exitCode == 0 {
				result.Status = JobStatusCompleted
			} else {
				result.Status = JobStatusFailed
			}
		}
	} else {
		result.Status = JobStatusFailed
		exitCode := 125
		result.ExitCode = &exitCode
	}

	if err := writeJSONAtomic(resultPath, result, 0600); err != nil {
		fmt.Fprintln(os.Stderr, "hec job-run: write result:", err)
		return 125
	}

	if mirrorSignal != 0 {
		signal.Stop(signalChannel)
		signal.Reset(mirrorSignal)
		if err := syscall.Kill(os.Getpid(), mirrorSignal); err != nil && !errors.Is(err, syscall.ESRCH) {
			fmt.Fprintln(os.Stderr, "hec job-run: mirror signal:", err)
		}
		time.Sleep(100 * time.Millisecond)
		return 128 + int(mirrorSignal)
	}
	if result.ExitCode != nil {
		return *result.ExitCode
	}
	if waitErr != nil {
		return 125
	}
	return 0
}
