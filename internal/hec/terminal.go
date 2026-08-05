package hec

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const terminalPaneFormat = "#{pane_pid}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_dead_signal}\t#{pane_width}\t#{pane_height}\t#{cursor_x}\t#{cursor_y}\t#{pane_current_command}\t#{pane_tty}"

func (d *Dispatcher) terminalOpen(ctx context.Context, raw map[string]any) Result {
	var args terminalOpenArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("terminal.open", "invalid_arguments", err.Error())
	}
	if err := normalizeTerminalOpenArgs(&args); err != nil {
		return failedResult("terminal.open", "invalid_arguments", err.Error())
	}
	if err := ensureStateRoot(d.terminalsDir); err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(d.tmuxSocket), 0755); err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}

	id, metadata, specPath, err := d.createTerminalState(args)
	if err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = d.killTerminalSession(context.Background(), metadata.Session)
			_ = os.RemoveAll(terminalDirectory(d.terminalsDir, id))
		}
	}()

	term := d.preferredTerminalType(ctx)
	if err := d.createTerminalSession(ctx, metadata, specPath, term); err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}
	if err := d.waitForTerminalSpecConsumption(ctx, specPath); err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}
	if err := d.configureTerminalSession(ctx, metadata, term); err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}
	if _, err := d.tmux(ctx, "wait-for", "-S", terminalWaitChannel(id)); err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}

	var pane terminalPaneInfo
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, pane, _, _, err = d.inspectTerminal(ctx, id)
		if err == nil && pane.TerminalStatus != TerminalStatusStarting {
			break
		}
		if err != nil || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return failedResult("terminal.open", "terminal_open_failed", err.Error())
	}

	cleanup = false
	handle := metadata.Handle
	result := newResult("terminal.open")
	result.OK = true
	result.Handle = &handle
	result.Result = map[string]any{
		"id":              id,
		"handle":          handle,
		"session":         metadata.Session,
		"terminal_status": pane.TerminalStatus,
		"width":           pane.Width,
		"height":          pane.Height,
	}
	if metadata.Name != "" {
		result.Result["name"] = metadata.Name
	}
	if pane.PanePID > 0 {
		result.Result["pane_pid"] = pane.PanePID
	}
	if pane.ExitCode != nil {
		result.Result["exit_code"] = *pane.ExitCode
	}
	if pane.Signal != nil {
		result.Result["signal"] = *pane.Signal
	}
	return result
}

func (d *Dispatcher) createTerminalState(args terminalOpenArgs) (string, TerminalMetadata, string, error) {
	var empty TerminalMetadata
	for attempts := 0; attempts < 8; attempts++ {
		id, err := newTerminalID()
		if err != nil {
			return "", empty, "", err
		}
		directory := terminalDirectory(d.terminalsDir, id)
		if err := os.Mkdir(directory, 0700); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return "", empty, "", err
		}
		remove := true
		defer func() {
			if remove {
				_ = os.RemoveAll(directory)
			}
		}()
		if err := os.Chmod(directory, 0700); err != nil {
			return "", empty, "", err
		}
		outputPath := terminalOutputPath(d.terminalsDir, id)
		if err := createPrivateFile(outputPath); err != nil {
			return "", empty, "", err
		}
		metadata := TerminalMetadata{
			ID:        id,
			Handle:    terminalHandle(id),
			Name:      args.Name,
			Session:   terminalSessionName(id),
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Width:     *args.Width,
			Height:    *args.Height,
		}
		if err := writeJSONAtomic(terminalMetadataPath(d.terminalsDir, id), metadata, 0600); err != nil {
			return "", empty, "", err
		}
		spec := terminalLaunchSpec{
			Command:     args.Command,
			Argv:        args.Argv,
			CWD:         args.CWD,
			Env:         args.Env,
			UnsetEnv:    args.UnsetEnv,
			TmuxSocket:  d.tmuxSocket,
			WaitChannel: terminalWaitChannel(id),
		}
		specPath := terminalSpecPath(d.terminalsDir, id)
		if err := writeJSONAtomic(specPath, spec, 0600); err != nil {
			return "", empty, "", err
		}
		remove = false
		return id, metadata, specPath, nil
	}
	return "", empty, "", errors.New("could not allocate terminal handle")
}

func terminalWaitChannel(id string) string {
	return "hec-terminal-" + id
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (d *Dispatcher) createTerminalSession(ctx context.Context, metadata TerminalMetadata, specPath, term string) error {
	lock, err := d.lockTmuxRuntime()
	if err != nil {
		return err
	}
	defer unlockTmuxRuntime(lock)

	launch := "exec " + shellSingleQuote(d.hecBinaryPath) + " terminal-run " + shellSingleQuote(specPath)
	alive := d.tmuxServerAlive(ctx)
	if !alive {
		state, err := d.tmuxScopeState(ctx)
		if err != nil {
			return err
		}
		if state == "active" || state == "activating" || state == "deactivating" {
			return fmt.Errorf("%s is %s without a usable tmux socket", d.tmuxScopeName(), state)
		}
		if state == "failed" {
			_, _ = exec.CommandContext(ctx, d.systemctlPath, "reset-failed", d.tmuxScopeName()).CombinedOutput()
		}
		if err := os.Remove(d.tmuxSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale tmux socket: %w", err)
		}
		arguments := []string{
			"--quiet",
			"--scope",
			"--unit=" + strings.TrimSuffix(d.tmuxScopeName(), ".scope"),
			"--slice=system.slice",
			"--collect",
			"--",
			"/usr/bin/env",
			"-i",
			"HOME=/root",
			"USER=root",
			"LOGNAME=root",
			"SHELL=/bin/bash",
			"PATH=" + terminalPathEnvironment,
			"LANG=C.UTF-8",
			d.tmuxPath,
			"-f", "/dev/null",
			"-S", d.tmuxSocket,
			"new-session",
			"-d",
			"-s", metadata.Session,
			"-x", strconv.Itoa(metadata.Width),
			"-y", strconv.Itoa(metadata.Height),
			"-e", "TERM=" + term,
			launch,
		}
		command := exec.CommandContext(ctx, d.systemdRunPath, arguments...)
		output, err := command.CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf("start dedicated tmux scope: %s", message)
		}
	} else {
		if _, err := d.tmux(ctx,
			"new-session", "-d",
			"-s", metadata.Session,
			"-x", strconv.Itoa(metadata.Width),
			"-y", strconv.Itoa(metadata.Height),
			"-e", "TERM="+term,
			launch,
		); err != nil {
			return err
		}
	}
	if !d.terminalSessionExists(ctx, metadata.Session) {
		return errors.New("tmux did not retain the new terminal session")
	}
	return nil
}

func (d *Dispatcher) lockTmuxRuntime() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(d.tmuxSocket), 0755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(d.tmuxSocket), ".tmux.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, err
	}
	return lock, nil
}

func unlockTmuxRuntime(lock *os.File) {
	if lock == nil {
		return
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func (d *Dispatcher) tmuxScopeName() string {
	name := d.tmuxScopeUnit
	if name == "" {
		name = "hec-tmux"
	}
	if !strings.HasSuffix(name, ".scope") {
		name += ".scope"
	}
	return name
}

func (d *Dispatcher) tmuxScopeState(ctx context.Context) (string, error) {
	command := exec.CommandContext(ctx, d.systemctlPath, "show", "--no-pager", "--property=LoadState", "--property=ActiveState", d.tmuxScopeName())
	output, err := command.CombinedOutput()
	loadState := ""
	activeState := ""
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			loadState = value
		case "ActiveState":
			activeState = value
		}
	}
	if loadState == "not-found" || loadState == "" {
		return "absent", nil
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("read tmux scope state: %s", message)
	}
	return activeState, nil
}

func (d *Dispatcher) preferredTerminalType(ctx context.Context) string {
	command := exec.CommandContext(ctx, d.infocmpPath, "tmux-256color")
	if err := command.Run(); err == nil {
		return "tmux-256color"
	}
	return "screen-256color"
}

func (d *Dispatcher) tmuxServerAlive(ctx context.Context) bool {
	if _, err := os.Stat(d.tmuxSocket); err != nil {
		return false
	}
	command := exec.CommandContext(ctx, d.tmuxPath, "-S", d.tmuxSocket, "list-sessions", "-F", "#{session_name}")
	return command.Run() == nil
}

func (d *Dispatcher) tmux(ctx context.Context, arguments ...string) ([]byte, error) {
	args := append([]string{"-S", d.tmuxSocket}, arguments...)
	command := exec.CommandContext(ctx, d.tmuxPath, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("tmux %s: %s", arguments[0], message)
	}
	return output, nil
}

func (d *Dispatcher) waitForTerminalSpecConsumption(ctx context.Context, specPath string) error {
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := os.Stat(specPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return errors.New("terminal launch helper did not consume its specification")
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (d *Dispatcher) configureTerminalSession(ctx context.Context, metadata TerminalMetadata, term string) error {
	commands := [][]string{
		{"set-option", "-g", "default-terminal", term},
		{"set-option", "-g", "history-limit", strconv.Itoa(TerminalHistoryLimit)},
		{"set-option", "-g", "exit-empty", "on"},
		{"set-option", "-g", "update-environment", ""},
		{"set-window-option", "-t", metadata.Session + ":0", "remain-on-exit", "on"},
		{"set-window-option", "-t", metadata.Session + ":0", "window-size", "manual"},
		{"resize-window", "-t", metadata.Session + ":0", "-x", strconv.Itoa(metadata.Width), "-y", strconv.Itoa(metadata.Height)},
	}
	for _, command := range commands {
		if _, err := d.tmux(ctx, command...); err != nil {
			return err
		}
	}
	outputPath := terminalOutputPath(d.terminalsDir, metadata.ID)
	pipeCommand := "exec /usr/bin/cat >> " + shellSingleQuote(outputPath)
	if _, err := d.tmux(ctx, "pipe-pane", "-o", "-t", metadata.Session+":0.0", pipeCommand); err != nil {
		return err
	}
	return nil
}

func (d *Dispatcher) terminalSessionExists(ctx context.Context, session string) bool {
	if _, err := os.Stat(d.tmuxSocket); err != nil {
		return false
	}
	command := exec.CommandContext(ctx, d.tmuxPath, "-S", d.tmuxSocket, "has-session", "-t", session)
	return command.Run() == nil
}

func (d *Dispatcher) inspectTerminal(ctx context.Context, id string) (TerminalMetadata, terminalPaneInfo, bool, bool, error) {
	var metadata TerminalMetadata
	var pane terminalPaneInfo
	stateExists := false
	if info, err := os.Stat(terminalDirectory(d.terminalsDir, id)); err == nil && info.IsDir() {
		stateExists = true
		loaded, loadErr := loadTerminalMetadata(d.terminalsDir, id)
		if loadErr != nil {
			return metadata, pane, true, false, loadErr
		}
		metadata = loaded
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return metadata, pane, false, false, err
	}

	session := terminalSessionName(id)
	sessionExists := d.terminalSessionExists(ctx, session)
	if !stateExists && !sessionExists {
		return metadata, pane, false, false, os.ErrNotExist
	}
	if !sessionExists {
		pane.TerminalStatus = TerminalStatusLost
		pane.Width = metadata.Width
		pane.Height = metadata.Height
		return metadata, pane, stateExists, false, nil
	}

	output, err := d.tmux(ctx, "display-message", "-p", "-t", session+":0.0", terminalPaneFormat)
	if err != nil {
		if !d.terminalSessionExists(ctx, session) && stateExists {
			pane.TerminalStatus = TerminalStatusLost
			pane.Width = metadata.Width
			pane.Height = metadata.Height
			return metadata, pane, true, false, nil
		}
		return metadata, pane, stateExists, true, err
	}
	pane, err = parseTerminalPaneInfo(strings.TrimSuffix(string(output), "\n"))
	if err != nil {
		return metadata, pane, stateExists, true, err
	}
	if !pane.Dead {
		if _, err := os.Stat(terminalSpecPath(d.terminalsDir, id)); err == nil {
			pane.TerminalStatus = TerminalStatusStarting
		} else {
			pane.TerminalStatus = TerminalStatusRunning
		}
	}
	if !stateExists {
		metadata = TerminalMetadata{
			ID:      id,
			Handle:  terminalHandle(id),
			Session: session,
			Width:   pane.Width,
			Height:  pane.Height,
		}
	}
	return metadata, pane, stateExists, true, nil
}

func parseTerminalPaneInfo(value string) (terminalPaneInfo, error) {
	var pane terminalPaneInfo
	fields := strings.Split(value, "\t")
	if len(fields) != 10 {
		return pane, fmt.Errorf("unexpected tmux pane format with %d fields", len(fields))
	}
	parseInt := func(index int, name string) (int, error) {
		parsed, err := strconv.Atoi(fields[index])
		if err != nil {
			return 0, fmt.Errorf("invalid tmux %s %q", name, fields[index])
		}
		return parsed, nil
	}
	var err error
	if pane.PanePID, err = parseInt(0, "pane_pid"); err != nil {
		return pane, err
	}
	pane.Dead = fields[1] == "1"
	if fields[1] != "0" && fields[1] != "1" {
		return pane, fmt.Errorf("invalid tmux pane_dead %q", fields[1])
	}
	if pane.Width, err = parseInt(4, "pane_width"); err != nil {
		return pane, err
	}
	if pane.Height, err = parseInt(5, "pane_height"); err != nil {
		return pane, err
	}
	if pane.CursorX, err = parseInt(6, "cursor_x"); err != nil {
		return pane, err
	}
	if pane.CursorY, err = parseInt(7, "cursor_y"); err != nil {
		return pane, err
	}
	pane.DeadStatus = fields[2]
	pane.DeadSignal = fields[3]
	pane.CurrentCommand = fields[8]
	pane.TTY = fields[9]
	if pane.Dead {
		pane.TerminalStatus = TerminalStatusExited
		if signal := normalizeTmuxSignal(pane.DeadSignal); signal != nil {
			pane.Signal = signal
		} else if pane.DeadStatus != "" {
			exitCode, err := strconv.Atoi(pane.DeadStatus)
			if err != nil {
				return pane, fmt.Errorf("invalid tmux pane_dead_status %q", pane.DeadStatus)
			}
			pane.ExitCode = &exitCode
		}
	}
	return pane, nil
}

func (d *Dispatcher) terminalList(ctx context.Context, raw map[string]any) Result {
	if len(raw) != 0 {
		return failedResult("terminal.list", "invalid_arguments", "terminal.list does not accept arguments")
	}
	ids := make(map[string]struct{})
	entries, err := os.ReadDir(d.terminalsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failedResult("terminal.list", "terminal_list_failed", err.Error())
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := parseTerminalHandle(terminalHandle(entry.Name())); err == nil {
			ids[entry.Name()] = struct{}{}
		}
	}
	if d.tmuxServerAlive(ctx) {
		output, err := d.tmux(ctx, "list-sessions", "-F", "#{session_name}")
		if err != nil {
			return failedResult("terminal.list", "terminal_list_failed", err.Error())
		}
		for _, session := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			id, err := parseTerminalSessionName(session)
			if err == nil {
				ids[id] = struct{}{}
			}
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	terminals := make([]any, 0, len(ordered))
	for _, id := range ordered {
		metadata, pane, _, _, err := d.inspectTerminal(ctx, id)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return failedResult("terminal.list", "terminal_list_failed", err.Error())
		}
		entry := map[string]any{
			"id":              id,
			"handle":          terminalHandle(id),
			"session":         terminalSessionName(id),
			"terminal_status": pane.TerminalStatus,
			"width":           pane.Width,
			"height":          pane.Height,
			"output_bytes":    fileSize(terminalOutputPath(d.terminalsDir, id)),
		}
		if metadata.Name != "" {
			entry["name"] = metadata.Name
		}
		if pane.TerminalStatus == TerminalStatusRunning {
			entry["pane_pid"] = pane.PanePID
			if pane.CurrentCommand != "" {
				entry["current_command"] = pane.CurrentCommand
			}
		}
		if pane.ExitCode != nil {
			entry["exit_code"] = *pane.ExitCode
		}
		if pane.Signal != nil {
			entry["signal"] = *pane.Signal
		}
		terminals = append(terminals, entry)
	}
	result := newResult("terminal.list")
	result.OK = true
	result.Result = map[string]any{"count": len(terminals), "terminals": terminals}
	return result
}

func (d *Dispatcher) terminalRead(ctx context.Context, raw map[string]any) Result {
	var args terminalReadArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("terminal.read", "invalid_arguments", err.Error())
	}
	id, err := parseTerminalHandle(args.Handle)
	if err != nil {
		return failedResult("terminal.read", "invalid_arguments", err.Error())
	}
	if args.Mode != "screen" && args.Mode != "output" {
		return failedResult("terminal.read", "invalid_arguments", "mode must be screen or output")
	}
	if args.Mode == "screen" && (args.Offset != nil || args.Limit != nil) {
		return failedResult("terminal.read", "invalid_arguments", "screen mode does not accept offset or limit")
	}
	metadata, pane, stateExists, sessionExists, err := d.inspectTerminal(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("terminal.read", "not_found", "terminal not found")
	}
	if err != nil {
		return failedResult("terminal.read", "terminal_read_failed", err.Error())
	}
	if args.Mode == "screen" {
		if !sessionExists || pane.TerminalStatus == TerminalStatusLost {
			return failedResult("terminal.read", "terminal_unavailable", "terminal screen is unavailable because the tmux session is lost")
		}
		stdout, encoding, truncated, err := d.captureTerminalScreen(ctx, metadata.Session)
		if err != nil {
			return failedResult("terminal.read", "terminal_read_failed", err.Error())
		}
		result := newResult("terminal.read")
		result.OK = true
		result.Handle = &args.Handle
		result.Stdout = stdout
		result.StdoutEncoding = encoding
		result.Truncated = truncated
		result.Result = map[string]any{
			"handle":          args.Handle,
			"mode":            "screen",
			"terminal_status": pane.TerminalStatus,
			"width":           pane.Width,
			"height":          pane.Height,
			"cursor_x":        pane.CursorX,
			"cursor_y":        pane.CursorY,
		}
		return result
	}
	if !stateExists {
		return failedResult("terminal.read", "terminal_read_failed", "terminal output state is unavailable")
	}
	offset := int64(0)
	if args.Offset != nil {
		offset = *args.Offset
	}
	limit := DefaultTerminalOutputLimit
	if args.Limit != nil {
		limit = *args.Limit
	}
	if offset < 0 {
		return failedResult("terminal.read", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if limit <= 0 || limit > MaximumRangeLimit {
		return failedResult("terminal.read", "invalid_arguments", "limit must be between 1 and 1048576")
	}
	file, err := os.Open(terminalOutputPath(d.terminalsDir, id))
	if err != nil {
		return failedResult("terminal.read", "terminal_read_failed", err.Error())
	}
	defer file.Close()
	data, next, total, eof, err := readRange(file, offset, limit)
	if err != nil {
		return failedResult("terminal.read", "terminal_read_failed", err.Error())
	}
	stdout, encoding := encodeOutput(data)
	result := newResult("terminal.read")
	result.OK = true
	result.Handle = &args.Handle
	result.Stdout = stdout
	result.StdoutEncoding = encoding
	result.Result = map[string]any{
		"handle":          args.Handle,
		"mode":            "output",
		"terminal_status": pane.TerminalStatus,
		"offset":          offset,
		"next_offset":     next,
		"total_bytes":     total,
		"eof":             eof,
		"encoding":        encoding,
	}
	return result
}

func (d *Dispatcher) captureTerminalScreen(ctx context.Context, session string) (string, string, bool, error) {
	command := exec.CommandContext(ctx, d.tmuxPath, "-S", d.tmuxSocket, "capture-pane", "-p", "-e", "-J", "-t", session+":0.0")
	limiter := newOutputLimiter(DefaultMaxOutputBytes)
	stdout := limiter.writer()
	var stderr bytes.Buffer
	command.Stdout = stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", "utf8", false, errors.New(message)
	}
	encoded, encoding := encodeOutput(stdout.bytes())
	return encoded, encoding, limiter.truncated(), nil
}

func (d *Dispatcher) terminalWrite(ctx context.Context, raw map[string]any) Result {
	var args terminalWriteArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("terminal.write", "invalid_arguments", err.Error())
	}
	id, err := parseTerminalHandle(args.Handle)
	if err != nil {
		return failedResult("terminal.write", "invalid_arguments", err.Error())
	}
	if (args.Data == nil) == (args.DataBase64 == nil) {
		return failedResult("terminal.write", "invalid_arguments", "terminal.write requires exactly one of data or data_base64")
	}
	var data []byte
	if args.Data != nil {
		data = []byte(*args.Data)
	} else {
		data, err = base64.StdEncoding.DecodeString(*args.DataBase64)
		if err != nil {
			return failedResult("terminal.write", "invalid_arguments", "data_base64 is not valid base64")
		}
	}
	if int64(len(data)) > MaximumTerminalWriteBytes {
		return failedResult("terminal.write", "invalid_arguments", "decoded write exceeds 1048576 bytes")
	}
	metadata, pane, stateExists, sessionExists, err := d.inspectTerminal(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("terminal.write", "not_found", "terminal not found")
	}
	if err != nil {
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	if !stateExists || !sessionExists || pane.TerminalStatus != TerminalStatusRunning {
		return failedResult("terminal.write", "terminal_unavailable", fmt.Sprintf("%s is %s", args.Handle, pane.TerminalStatus))
	}

	temp, err := os.CreateTemp(terminalDirectory(d.terminalsDir, id), ".paste-*")
	if err != nil {
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	tempPath := temp.Name()
	bufferID, idErr := secureID()
	if idErr != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return failedResult("terminal.write", "terminal_write_failed", idErr.Error())
	}
	bufferName := "hec-paste-" + bufferID
	defer func() {
		_, _ = d.tmux(context.Background(), "delete-buffer", "-b", bufferName)
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	if err := temp.Close(); err != nil {
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	if _, err := d.tmux(ctx, "load-buffer", "-b", bufferName, tempPath); err != nil {
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	if _, err := d.tmux(ctx, "paste-buffer", "-r", "-b", bufferName, "-t", metadata.Session+":0.0"); err != nil {
		return failedResult("terminal.write", "terminal_write_failed", err.Error())
	}
	_, _ = d.tmux(ctx, "delete-buffer", "-b", bufferName)
	updatedStatus := pane.TerminalStatus
	if _, updated, _, _, inspectErr := d.inspectTerminal(ctx, id); inspectErr == nil {
		updatedStatus = updated.TerminalStatus
	}
	result := newResult("terminal.write")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":          args.Handle,
		"bytes_written":   len(data),
		"terminal_status": updatedStatus,
	}
	return result
}

func (d *Dispatcher) terminalResize(ctx context.Context, raw map[string]any) Result {
	var args terminalResizeArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("terminal.resize", "invalid_arguments", err.Error())
	}
	id, err := parseTerminalHandle(args.Handle)
	if err != nil {
		return failedResult("terminal.resize", "invalid_arguments", err.Error())
	}
	if err := validateTerminalDimension(args.Width, "width"); err != nil {
		return failedResult("terminal.resize", "invalid_arguments", err.Error())
	}
	if err := validateTerminalDimension(args.Height, "height"); err != nil {
		return failedResult("terminal.resize", "invalid_arguments", err.Error())
	}
	metadata, pane, _, sessionExists, err := d.inspectTerminal(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("terminal.resize", "not_found", "terminal not found")
	}
	if err != nil {
		return failedResult("terminal.resize", "terminal_resize_failed", err.Error())
	}
	if !sessionExists || pane.TerminalStatus != TerminalStatusRunning {
		return failedResult("terminal.resize", "terminal_unavailable", fmt.Sprintf("%s is %s", args.Handle, pane.TerminalStatus))
	}
	if _, err := d.tmux(ctx, "resize-window", "-t", metadata.Session+":0", "-x", strconv.Itoa(args.Width), "-y", strconv.Itoa(args.Height)); err != nil {
		return failedResult("terminal.resize", "terminal_resize_failed", err.Error())
	}
	_, pane, _, _, err = d.inspectTerminal(ctx, id)
	if err != nil {
		return failedResult("terminal.resize", "terminal_resize_failed", err.Error())
	}
	result := newResult("terminal.resize")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":          args.Handle,
		"width":           pane.Width,
		"height":          pane.Height,
		"terminal_status": pane.TerminalStatus,
	}
	return result
}

func (d *Dispatcher) terminalSignal(ctx context.Context, raw map[string]any) Result {
	var args terminalSignalArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("terminal.signal", "invalid_arguments", err.Error())
	}
	id, err := parseTerminalHandle(args.Handle)
	if err != nil {
		return failedResult("terminal.signal", "invalid_arguments", err.Error())
	}
	linuxSignal, signalName, err := parseLinuxSignalName(args.Signal)
	if err != nil {
		return failedResult("terminal.signal", "invalid_arguments", err.Error())
	}
	_, pane, _, sessionExists, err := d.inspectTerminal(ctx, id)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("terminal.signal", "not_found", "terminal not found")
	}
	if err != nil {
		return failedResult("terminal.signal", "terminal_signal_failed", err.Error())
	}
	if !sessionExists || pane.TerminalStatus != TerminalStatusRunning {
		return failedResult("terminal.signal", "terminal_unavailable", fmt.Sprintf("%s is %s", args.Handle, pane.TerminalStatus))
	}
	pgid, err := d.terminalForegroundProcessGroup(ctx, pane)
	if err != nil {
		return failedResult("terminal.signal", "terminal_signal_failed", err.Error())
	}
	if err := syscall.Kill(-pgid, linuxSignal); err != nil {
		return failedResult("terminal.signal", "terminal_signal_failed", err.Error())
	}
	status := pane.TerminalStatus
	if _, updated, _, _, inspectErr := d.inspectTerminal(ctx, id); inspectErr == nil {
		status = updated.TerminalStatus
	}
	result := newResult("terminal.signal")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":          args.Handle,
		"signal":          signalName,
		"terminal_status": status,
		"foreground_pgid": pgid,
	}
	return result
}

func (d *Dispatcher) terminalForegroundProcessGroup(ctx context.Context, pane terminalPaneInfo) (int, error) {
	if !strings.HasPrefix(pane.TTY, "/dev/pts/") || filepath.Clean(pane.TTY) != pane.TTY {
		return 0, errors.New("tmux reported an invalid pane tty")
	}
	paneStat, err := readProcTerminalStat(pane.PanePID)
	if err != nil {
		return 0, fmt.Errorf("read pane process state: %w", err)
	}
	file, ioctlErr := os.OpenFile(pane.TTY, os.O_RDONLY|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	pgid := 0
	if ioctlErr == nil {
		pgid, ioctlErr = unix.IoctlGetInt(int(file.Fd()), unix.TIOCGPGRP)
		_ = file.Close()
	}
	if ioctlErr != nil || pgid <= 1 {
		pgid = paneStat.ForegroundPGRP
	}
	if pgid <= 1 || pgid == syscall.Getpgrp() {
		return 0, errors.New("refusing unsafe terminal process group")
	}
	leaderStat, err := readProcTerminalStat(pgid)
	if err != nil {
		return 0, fmt.Errorf("validate terminal foreground process group: %w", err)
	}
	if leaderStat.ProcessGroup != pgid || leaderStat.Session != paneStat.Session || leaderStat.TTYNumber == 0 || leaderStat.TTYNumber != paneStat.TTYNumber {
		return 0, errors.New("terminal foreground process group does not belong to the pane tty session")
	}
	output, err := d.tmux(ctx, "display-message", "-p", "#{pid}")
	if err != nil {
		return 0, err
	}
	serverPID, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || serverPID <= 1 {
		return 0, errors.New("tmux reported an invalid server pid")
	}
	serverPGID, err := syscall.Getpgid(serverPID)
	if err != nil {
		return 0, err
	}
	if pgid == serverPGID {
		return 0, errors.New("refusing to signal the tmux server process group")
	}
	return pgid, nil
}

type procTerminalStat struct {
	ProcessGroup   int
	Session        int
	TTYNumber      int
	ForegroundPGRP int
}

func readProcTerminalStat(pid int) (procTerminalStat, error) {
	var result procTerminalStat
	if pid <= 1 {
		return result, errors.New("invalid process id")
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return result, err
	}
	closing := bytes.LastIndex(data, []byte(") "))
	if closing < 0 {
		return result, errors.New("malformed /proc process stat")
	}
	fields := strings.Fields(string(data[closing+2:]))
	if len(fields) < 6 {
		return result, errors.New("incomplete /proc process stat")
	}
	parse := func(index int, name string) (int, error) {
		value, err := strconv.Atoi(fields[index])
		if err != nil {
			return 0, fmt.Errorf("invalid %s in /proc process stat", name)
		}
		return value, nil
	}
	if result.ProcessGroup, err = parse(2, "process group"); err != nil {
		return result, err
	}
	if result.Session, err = parse(3, "session"); err != nil {
		return result, err
	}
	if result.TTYNumber, err = parse(4, "tty number"); err != nil {
		return result, err
	}
	if result.ForegroundPGRP, err = parse(5, "foreground process group"); err != nil {
		return result, err
	}
	return result, nil
}

func (d *Dispatcher) terminalClose(ctx context.Context, raw map[string]any) Result {
	var args terminalHandleArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("terminal.close", "invalid_arguments", err.Error())
	}
	id, err := parseTerminalHandle(args.Handle)
	if err != nil {
		return failedResult("terminal.close", "invalid_arguments", err.Error())
	}
	stateExists := false
	if _, err := os.Stat(terminalDirectory(d.terminalsDir, id)); err == nil {
		stateExists = true
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failedResult("terminal.close", "terminal_close_failed", err.Error())
	}
	session := terminalSessionName(id)
	hadSession := d.terminalSessionExists(ctx, session)
	if !stateExists && !hadSession {
		return failedResult("terminal.close", "not_found", "terminal not found")
	}
	if hadSession {
		if err := d.killTerminalSession(ctx, session); err != nil && d.terminalSessionExists(ctx, session) {
			return failedResult("terminal.close", "terminal_close_failed", err.Error())
		}
		deadline := time.Now().Add(2 * time.Second)
		for d.terminalSessionExists(ctx, session) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if d.terminalSessionExists(ctx, session) {
			return failedResult("terminal.close", "terminal_close_failed", "tmux session did not close")
		}
	}
	if err := os.RemoveAll(terminalDirectory(d.terminalsDir, id)); err != nil {
		return failedResult("terminal.close", "terminal_close_failed", err.Error())
	}
	result := newResult("terminal.close")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":      args.Handle,
		"closed":      true,
		"had_session": hadSession,
	}
	return result
}

func (d *Dispatcher) killTerminalSession(ctx context.Context, session string) error {
	if !d.terminalSessionExists(ctx, session) {
		return nil
	}
	_, err := d.tmux(ctx, "kill-session", "-t", session)
	return err
}
