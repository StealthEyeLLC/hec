package hec

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func RunTerminal(specPath string) int {
	spec, err := decodeTerminalLaunchSpec(specPath)
	if removeErr := os.Remove(specPath); err == nil && removeErr != nil {
		err = fmt.Errorf("remove terminal launch specification: %w", removeErr)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hec terminal-run:", err)
		return 125
	}

	environment, err := buildTerminalEnvironment(os.Getenv("TERM"), spec.Env, spec.UnsetEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hec terminal-run:", err)
		return 125
	}
	if err := os.Chdir(spec.CWD); err != nil {
		fmt.Fprintln(os.Stderr, "hec terminal-run: change directory:", err)
		return 125
	}

	gate := exec.Command("/usr/bin/tmux", "-S", spec.TmuxSocket, "wait-for", spec.WaitChannel)
	gate.Env = environment
	gate.Stdout = os.Stdout
	gate.Stderr = os.Stderr
	if err := gate.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "hec terminal-run: wait for launch gate:", err)
		return 125
	}

	argv := []string{"/bin/bash", "-l"}
	if spec.Command != nil {
		argv = []string{"/bin/bash", "-lc", *spec.Command}
	} else if spec.Argv != nil {
		argv = spec.Argv
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "hec terminal-run: find executable:", err)
		return 127
	}
	if err := syscall.Exec(path, argv, environment); err != nil {
		fmt.Fprintln(os.Stderr, "hec terminal-run: exec:", err)
		return 126
	}
	return 0
}
