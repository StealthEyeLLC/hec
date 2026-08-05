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
	"strings"
	"syscall"
)

type fileStatArgs struct {
	Path string `json:"path"`
}

type fileListArgs struct {
	Path   string `json:"path"`
	Offset *int64 `json:"offset"`
	Limit  *int64 `json:"limit"`
}

type fileReadArgs struct {
	Path   string `json:"path"`
	Offset *int64 `json:"offset"`
	Limit  *int64 `json:"limit"`
}

type fileWriteArgs struct {
	Path          string  `json:"path"`
	Content       *string `json:"content"`
	ContentBase64 *string `json:"content_base64"`
}

type filePatchArgs struct {
	CWD   string `json:"cwd"`
	Patch string `json:"patch"`
	Strip *int   `json:"strip"`
}

type fileRemoveArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	MissingOK bool   `json:"missing_ok"`
}

func (d *Dispatcher) fileStat(_ context.Context, raw map[string]any) Result {
	var args fileStatArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.stat", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("file.stat", "invalid_arguments", err.Error())
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("file.stat", "not_found", "path not found")
	}
	if err != nil {
		return failedResult("file.stat", "stat_failed", err.Error())
	}
	uid, gid := fileOwner(info)
	metadata := map[string]any{
		"path":        path,
		"name":        info.Name(),
		"type":        fileType(info),
		"size":        info.Size(),
		"mode":        fileModeString(info),
		"uid":         uid,
		"gid":         gid,
		"modified_at": modificationTime(info),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return failedResult("file.stat", "readlink_failed", err.Error())
		}
		metadata["symlink_target"] = target
	}
	result := newResult("file.stat")
	result.OK = true
	result.Result = metadata
	return result
}

func (d *Dispatcher) fileList(_ context.Context, raw map[string]any) Result {
	var args fileListArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.list", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("file.list", "invalid_arguments", err.Error())
	}
	offset := int64(0)
	if args.Offset != nil {
		offset = *args.Offset
	}
	limit := int64(256)
	if args.Limit != nil {
		limit = *args.Limit
	}
	if offset < 0 {
		return failedResult("file.list", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if limit <= 0 || limit > 4096 {
		return failedResult("file.list", "invalid_arguments", "limit must be between 1 and 4096")
	}
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("file.list", "not_found", "directory not found")
	}
	if err != nil {
		return failedResult("file.list", "list_failed", err.Error())
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	total := int64(len(entries))
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	listed := make([]map[string]any, 0, end-start)
	for _, entry := range entries[start:end] {
		entryPath := filepath.Join(path, entry.Name())
		info, err := os.Lstat(entryPath)
		if err != nil {
			return failedResult("file.list", "stat_failed", err.Error())
		}
		listed = append(listed, map[string]any{
			"name":        entry.Name(),
			"path":        entryPath,
			"type":        fileType(info),
			"size":        info.Size(),
			"mode":        fileModeString(info),
			"modified_at": modificationTime(info),
		})
	}
	result := newResult("file.list")
	result.OK = true
	result.Result = map[string]any{
		"path":          path,
		"offset":        offset,
		"next_offset":   end,
		"total_entries": total,
		"eof":           end >= total,
		"entries":       listed,
	}
	return result
}

func (d *Dispatcher) fileRead(_ context.Context, raw map[string]any) Result {
	var args fileReadArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.read", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("file.read", "invalid_arguments", err.Error())
	}
	offset := int64(0)
	if args.Offset != nil {
		offset = *args.Offset
	}
	limit := DefaultRangeLimit
	if args.Limit != nil {
		limit = *args.Limit
	}
	if offset < 0 {
		return failedResult("file.read", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if limit <= 0 || limit > MaximumRangeLimit {
		return failedResult("file.read", "invalid_arguments", "limit must be between 1 and 1048576")
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("file.read", "not_found", "file not found")
	}
	if err != nil {
		return failedResult("file.read", "read_failed", err.Error())
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return failedResult("file.read", "read_failed", err.Error())
	}
	if info.IsDir() {
		return failedResult("file.read", "is_directory", "path is a directory")
	}
	data, next, total, eof, err := readRange(file, offset, limit)
	if err != nil {
		return failedResult("file.read", "read_failed", err.Error())
	}
	encoded, encoding := encodeData(data)
	result := newResult("file.read")
	result.OK = true
	result.Result = map[string]any{
		"path":        path,
		"offset":      offset,
		"next_offset": next,
		"total_bytes": total,
		"eof":         eof,
		"encoding":    encoding,
		"data":        encoded,
	}
	return result
}

func (d *Dispatcher) fileWrite(_ context.Context, raw map[string]any) Result {
	var args fileWriteArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.write", "invalid_arguments", err.Error())
	}
	data, err := decodeContent(args.Content, args.ContentBase64, "file.write")
	if err != nil {
		return failedResult("file.write", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("file.write", "invalid_arguments", err.Error())
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return failedResult("file.write", "write_failed", err.Error())
	}
	if !parentInfo.IsDir() {
		return failedResult("file.write", "write_failed", "parent is not a directory")
	}

	replaced := false
	mode := os.FileMode(0600)
	uid, gid := -1, -1
	if info, err := os.Lstat(path); err == nil {
		if info.IsDir() {
			return failedResult("file.write", "is_directory", "path is a directory")
		}
		replaced = true
		if info.Mode().IsRegular() {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				mode = os.FileMode(stat.Mode & 07777)
				uid = int(stat.Uid)
				gid = int(stat.Gid)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return failedResult("file.write", "write_failed", err.Error())
	}

	temp, err := os.CreateTemp(parent, "."+filepath.Base(path)+".hec-*")
	if err != nil {
		return failedResult("file.write", "write_failed", err.Error())
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if uid >= 0 {
		if err := temp.Chown(uid, gid); err != nil {
			_ = temp.Close()
			return failedResult("file.write", "write_failed", err.Error())
		}
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return failedResult("file.write", "write_failed", err.Error())
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return failedResult("file.write", "write_failed", err.Error())
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return failedResult("file.write", "write_failed", err.Error())
	}
	if err := temp.Close(); err != nil {
		return failedResult("file.write", "write_failed", err.Error())
	}
	if err := os.Rename(tempPath, path); err != nil {
		return failedResult("file.write", "write_failed", err.Error())
	}
	removeTemp = false

	result := newResult("file.write")
	result.OK = true
	result.Result = map[string]any{
		"path":          path,
		"bytes_written": len(data),
		"replaced":      replaced,
	}
	return result
}

func (d *Dispatcher) fileAppend(_ context.Context, raw map[string]any) Result {
	var args fileWriteArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.append", "invalid_arguments", err.Error())
	}
	data, err := decodeContent(args.Content, args.ContentBase64, "file.append")
	if err != nil {
		return failedResult("file.append", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("file.append", "invalid_arguments", err.Error())
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return failedResult("file.append", "append_failed", err.Error())
	}
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return failedResult("file.append", "append_failed", writeErr.Error())
	}
	if written != len(data) {
		return failedResult("file.append", "append_failed", "short append")
	}
	if closeErr != nil {
		return failedResult("file.append", "append_failed", closeErr.Error())
	}
	info, err := os.Stat(path)
	if err != nil {
		return failedResult("file.append", "append_failed", err.Error())
	}
	result := newResult("file.append")
	result.OK = true
	result.Result = map[string]any{
		"path":           path,
		"bytes_appended": written,
		"total_bytes":    info.Size(),
	}
	return result
}

func (d *Dispatcher) filePatch(ctx context.Context, raw map[string]any) Result {
	var args filePatchArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.patch", "invalid_arguments", err.Error())
	}
	if args.Patch == "" {
		return failedResult("file.patch", "invalid_arguments", "patch is required")
	}
	cwd := args.CWD
	if cwd == "" {
		cwd = DefaultCWD
	}
	resolved, err := resolveFilesystemPath(cwd)
	if err != nil {
		return failedResult("file.patch", "invalid_arguments", err.Error())
	}
	cwd = resolved
	strip := 1
	if args.Strip != nil {
		strip = *args.Strip
	}
	if strip < 0 || strip > 16 {
		return failedResult("file.patch", "invalid_arguments", "strip must be between 0 and 16")
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return failedResult("file.patch", "invalid_arguments", "cwd must be a directory")
	}

	useGit := false
	probe := exec.CommandContext(ctx, d.gitPath, "-C", cwd, "rev-parse", "--is-inside-work-tree")
	if output, err := probe.Output(); err == nil && strings.TrimSpace(string(output)) == "true" {
		useGit = true
	}

	var command *exec.Cmd
	toolName := "patch"
	if useGit {
		toolName = "git apply"
		command = exec.CommandContext(ctx, d.gitPath, "-C", cwd, "apply", fmt.Sprintf("-p%d", strip), "--whitespace=nowarn", "-")
	} else {
		command = exec.CommandContext(ctx, d.patchPath, "-d", cwd, fmt.Sprintf("-p%d", strip), "--batch", "--forward")
	}
	command.Stdin = strings.NewReader(args.Patch)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()

	result := newResult("file.patch")
	result.Stdout, result.StdoutEncoding = encodeOutput(stdout.Bytes())
	result.Stderr, result.StderrEncoding = encodeOutput(stderr.Bytes())
	if command.ProcessState != nil {
		setProcessOutcome(&result, command.ProcessState)
	}
	if runErr != nil {
		result.Status = StatusFailed
		result.Error = &ErrorDetail{Code: "patch_failed", Message: toolName + " failed"}
		return result
	}
	result.OK = true
	result.Result = map[string]any{"cwd": cwd, "applied": true}
	return result
}

func (d *Dispatcher) fileRemove(_ context.Context, raw map[string]any) Result {
	var args fileRemoveArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("file.remove", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("file.remove", "invalid_arguments", err.Error())
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if args.MissingOK {
			result := newResult("file.remove")
			result.OK = true
			result.Result = map[string]any{"path": path, "removed": false}
			return result
		}
		return failedResult("file.remove", "not_found", "path not found")
	} else if err != nil {
		return failedResult("file.remove", "remove_failed", err.Error())
	}
	if args.Recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return failedResult("file.remove", "remove_failed", err.Error())
	}
	result := newResult("file.remove")
	result.OK = true
	result.Result = map[string]any{"path": path, "removed": true}
	return result
}

func decodeChunk(data string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, errors.New("data_base64 is not valid base64")
	}
	if int64(len(decoded)) > MaximumChunkBytes {
		return nil, errors.New("decoded chunk exceeds 1048576 bytes")
	}
	return decoded, nil
}
