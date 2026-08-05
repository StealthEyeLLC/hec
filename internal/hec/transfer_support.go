package hec

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	DefaultRangeLimit = int64(262144)
	MaximumRangeLimit = int64(1048576)
	MaximumChunkBytes = int64(1048576)

	UploadRootDir   = "/var/lib/hec/uploads"
	ArtifactRootDir = "/var/lib/hec/artifacts"
)

type ResourceDescriptor struct {
	URI       string `json:"uri"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
}

func secureID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate secure id: %w", err)
	}
	return hex.EncodeToString(data), nil
}

func parseTypedHandle(handle, prefix string) (string, error) {
	wantedPrefix := prefix + ":"
	if !strings.HasPrefix(handle, wantedPrefix) {
		return "", fmt.Errorf("invalid %s handle", prefix)
	}
	id := strings.TrimPrefix(handle, wantedPrefix)
	if len(id) != 32 {
		return "", fmt.Errorf("invalid %s handle", prefix)
	}
	if strings.ToLower(id) != id {
		return "", fmt.Errorf("invalid %s handle", prefix)
	}
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 16 {
		return "", fmt.Errorf("invalid %s handle", prefix)
	}
	return id, nil
}

func resolveFilesystemPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(path, '\x00') {
		return "", errors.New("path contains NUL")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(DefaultCWD, path)), nil
}

func validateBasename(name, field string) error {
	if name == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.ContainsAny(name, "/\\") || filepath.Base(name) != name || name == "." || name == ".." {
		return fmt.Errorf("%s must be a basename", field)
	}
	if strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("%s contains NUL", field)
	}
	return nil
}

func decodeContent(content, contentBase64 *string, operation string) ([]byte, error) {
	if (content == nil) == (contentBase64 == nil) {
		return nil, fmt.Errorf("%s requires exactly one of content or content_base64", operation)
	}
	if content != nil {
		return []byte(*content), nil
	}
	data, err := base64.StdEncoding.DecodeString(*contentBase64)
	if err != nil {
		return nil, errors.New("content_base64 is not valid base64")
	}
	return data, nil
}

func encodeData(data []byte) (string, string) {
	if utf8.Valid(data) {
		return string(data), "utf8"
	}
	return base64.StdEncoding.EncodeToString(data), "base64"
}

func readRange(file *os.File, offset, limit int64) ([]byte, int64, int64, bool, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, offset, 0, false, err
	}
	total := info.Size()
	if offset >= total {
		return []byte{}, offset, total, true, nil
	}
	remaining := total - offset
	if limit > remaining {
		limit = remaining
	}
	data := make([]byte, int(limit))
	n, err := file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, offset, total, false, err
	}
	data = data[:n]
	next := offset + int64(n)
	return data, next, total, next >= total, nil
}

func fileType(info os.FileInfo) string {
	mode := info.Mode()
	switch {
	case mode.IsRegular():
		return "file"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeDevice != 0:
		return "device"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	default:
		return "other"
	}
}

func fileModeString(info os.FileInfo) string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%04o", stat.Mode&07777)
	}
	return fmt.Sprintf("%04o", info.Mode().Perm())
}

func fileOwner(info os.FileInfo) (uint32, uint32) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Uid, stat.Gid
	}
	return 0, 0
}

func modificationTime(info os.FileInfo) string {
	return info.ModTime().UTC().Format(time.RFC3339Nano)
}

func ensureStateRoot(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}

func publishNoReplace(source, destination string) error {
	err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	if err := os.Link(source, destination); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func publishPath(source, destination string, overwrite bool) error {
	if overwrite {
		return os.Rename(source, destination)
	}
	return publishNoReplace(source, destination)
}

func copyFileToAtomicDestination(source, destination string, overwrite bool) (int64, error) {
	parent := filepath.Dir(destination)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return 0, err
	}
	if !parentInfo.IsDir() {
		return 0, fmt.Errorf("destination parent is not a directory")
	}
	if !overwrite {
		if _, err := os.Lstat(destination); err == nil {
			return 0, os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}

	input, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer input.Close()

	temp, err := os.CreateTemp(parent, "."+filepath.Base(destination)+".hec-*")
	if err != nil {
		return 0, err
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return 0, err
	}
	written, err := io.Copy(temp, input)
	if err != nil {
		_ = temp.Close()
		return 0, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return 0, err
	}
	if err := temp.Close(); err != nil {
		return 0, err
	}
	if err := publishPath(tempPath, destination, overwrite); err != nil {
		return 0, err
	}
	removeTemp = false
	return written, nil
}

func moveFileToAtomicDestination(source, destination string, overwrite bool) (int64, error) {
	info, err := os.Stat(source)
	if err != nil {
		return 0, err
	}
	if !overwrite {
		if _, err := os.Lstat(destination); err == nil {
			return 0, os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	if err := publishPath(source, destination, overwrite); err == nil {
		return info.Size(), nil
	} else if !errors.Is(err, unix.EXDEV) {
		return 0, err
	}
	return copyFileToAtomicDestination(source, destination, overwrite)
}
