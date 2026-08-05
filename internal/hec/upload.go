package hec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type uploadMetadata struct {
	ID             string `json:"id"`
	Handle         string `json:"handle"`
	Filename       string `json:"filename"`
	ExpectedSize   *int64 `json:"expected_size,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	CurrentBytes   int64  `json:"current_bytes"`
}

type uploadBeginArgs struct {
	Filename string `json:"filename"`
	Size     *int64 `json:"size"`
	SHA256   string `json:"sha256"`
}

type uploadChunkArgs struct {
	Handle     string  `json:"handle"`
	Offset     *int64  `json:"offset"`
	DataBase64 *string `json:"data_base64"`
}

type uploadFinishArgs struct {
	Handle      string  `json:"handle"`
	Destination *string `json:"destination"`
	Overwrite   bool    `json:"overwrite"`
	Artifact    *bool   `json:"artifact"`
	Name        string  `json:"name"`
	MediaType   string  `json:"media_type"`
}

type uploadAbortArgs struct {
	Handle string `json:"handle"`
}

func uploadDirectory(root, id string) string {
	return filepath.Join(root, id)
}

func uploadDataPath(root, id string) string {
	return filepath.Join(uploadDirectory(root, id), "data")
}

func uploadMetadataPath(root, id string) string {
	return filepath.Join(uploadDirectory(root, id), "metadata.json")
}

func loadUploadMetadata(root, id string) (uploadMetadata, error) {
	var metadata uploadMetadata
	data, err := os.ReadFile(uploadMetadataPath(root, id))
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, err
	}
	if metadata.ID != id || metadata.Handle != "upload:"+id {
		return metadata, errors.New("upload metadata does not match handle")
	}
	return metadata, nil
}

func validateSHA256(value string) error {
	if value == "" {
		return nil
	}
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return errors.New("sha256 must be one lowercase hexadecimal SHA-256 digest")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("sha256 must be one lowercase hexadecimal SHA-256 digest")
	}
	return nil
}

func (d *Dispatcher) uploadBegin(_ context.Context, raw map[string]any) Result {
	var args uploadBeginArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("upload.begin", "invalid_arguments", err.Error())
	}
	if err := validateBasename(args.Filename, "filename"); err != nil {
		return failedResult("upload.begin", "invalid_arguments", err.Error())
	}
	if args.Size != nil && *args.Size < 0 {
		return failedResult("upload.begin", "invalid_arguments", "size must be greater than or equal to zero")
	}
	if err := validateSHA256(args.SHA256); err != nil {
		return failedResult("upload.begin", "invalid_arguments", err.Error())
	}
	if err := ensureStateRoot(d.uploadsDir); err != nil {
		return failedResult("upload.begin", "upload_failed", err.Error())
	}

	for attempts := 0; attempts < 4; attempts++ {
		id, err := secureID()
		if err != nil {
			return failedResult("upload.begin", "upload_failed", err.Error())
		}
		handle := "upload:" + id
		tempDir := filepath.Join(d.uploadsDir, ".tmp-"+id)
		finalDir := uploadDirectory(d.uploadsDir, id)
		if err := os.Mkdir(tempDir, 0700); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return failedResult("upload.begin", "upload_failed", err.Error())
		}
		removeTemp := true
		func() {
			defer func() {
				if removeTemp {
					_ = os.RemoveAll(tempDir)
				}
			}()
			dataFile, createErr := os.OpenFile(filepath.Join(tempDir, "data"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if createErr != nil {
				err = createErr
				return
			}
			if closeErr := dataFile.Close(); closeErr != nil {
				err = closeErr
				return
			}
			metadata := uploadMetadata{
				ID:             id,
				Handle:         handle,
				Filename:       args.Filename,
				ExpectedSize:   args.Size,
				ExpectedSHA256: args.SHA256,
				CurrentBytes:   0,
			}
			if writeErr := writeJSONAtomic(filepath.Join(tempDir, "metadata.json"), metadata, 0600); writeErr != nil {
				err = writeErr
				return
			}
			if renameErr := os.Rename(tempDir, finalDir); renameErr != nil {
				err = renameErr
				return
			}
			removeTemp = false
		}()
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return failedResult("upload.begin", "upload_failed", err.Error())
		}
		result := newResult("upload.begin")
		result.OK = true
		result.Handle = &handle
		result.Result = map[string]any{
			"id":          id,
			"handle":      handle,
			"filename":    args.Filename,
			"next_offset": 0,
		}
		return result
	}
	return failedResult("upload.begin", "upload_failed", "could not allocate upload handle")
}

func (d *Dispatcher) uploadChunk(_ context.Context, raw map[string]any) Result {
	var args uploadChunkArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("upload.chunk", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("upload.chunk", "invalid_arguments", "handle is required")
	}
	if args.Offset == nil {
		return failedResult("upload.chunk", "invalid_arguments", "offset is required")
	}
	if *args.Offset < 0 {
		return failedResult("upload.chunk", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if args.DataBase64 == nil {
		return failedResult("upload.chunk", "invalid_arguments", "data_base64 is required")
	}
	data, err := decodeChunk(*args.DataBase64)
	if err != nil {
		return failedResult("upload.chunk", "invalid_arguments", err.Error())
	}
	id, err := parseTypedHandle(args.Handle, "upload")
	if err != nil {
		return failedResult("upload.chunk", "invalid_arguments", err.Error())
	}
	file, err := os.OpenFile(uploadDataPath(d.uploadsDir, id), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("upload.chunk", "not_found", "upload not found")
	}
	if err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	metadata, err := loadUploadMetadata(d.uploadsDir, id)
	if err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	info, err := file.Stat()
	if err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	if *args.Offset > info.Size() {
		return failedResult("upload.chunk", "invalid_offset", "offset is beyond current upload data")
	}
	written, err := file.WriteAt(data, *args.Offset)
	if err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	if written != len(data) {
		return failedResult("upload.chunk", "upload_failed", "short chunk write")
	}
	if err := file.Sync(); err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	info, err = file.Stat()
	if err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	metadata.CurrentBytes = info.Size()
	if err := writeJSONAtomic(uploadMetadataPath(d.uploadsDir, id), metadata, 0600); err != nil {
		return failedResult("upload.chunk", "upload_failed", err.Error())
	}
	result := newResult("upload.chunk")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":        args.Handle,
		"offset":        *args.Offset,
		"bytes_written": written,
		"next_offset":   *args.Offset + int64(written),
		"total_bytes":   info.Size(),
	}
	return result
}

func verifyUpload(file *os.File, metadata uploadMetadata) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if metadata.ExpectedSize != nil && info.Size() != *metadata.ExpectedSize {
		return fmt.Errorf("upload size is %d bytes; expected %d", info.Size(), *metadata.ExpectedSize)
	}
	if metadata.ExpectedSHA256 != "" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return err
		}
		actual := hex.EncodeToString(hash.Sum(nil))
		if actual != metadata.ExpectedSHA256 {
			return fmt.Errorf("upload SHA-256 is %s; expected %s", actual, metadata.ExpectedSHA256)
		}
	}
	return nil
}

func (d *Dispatcher) uploadFinish(ctx context.Context, raw map[string]any) Result {
	var args uploadFinishArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("upload.finish", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("upload.finish", "invalid_arguments", "handle is required")
	}
	artifactTarget := args.Artifact != nil && *args.Artifact
	if args.Artifact != nil && !*args.Artifact {
		return failedResult("upload.finish", "invalid_arguments", "artifact must be true when provided")
	}
	if (args.Destination != nil) == artifactTarget {
		return failedResult("upload.finish", "invalid_arguments", "upload.finish requires exactly one of destination or artifact true")
	}
	if args.Destination != nil {
		if !filepath.IsAbs(*args.Destination) {
			return failedResult("upload.finish", "invalid_arguments", "destination must be absolute")
		}
		if args.Name != "" || args.MediaType != "" {
			return failedResult("upload.finish", "invalid_arguments", "name and media_type apply only to artifact completion")
		}
	} else if args.Overwrite {
		return failedResult("upload.finish", "invalid_arguments", "overwrite applies only to destination completion")
	}
	if args.Name != "" {
		if err := validateBasename(args.Name, "name"); err != nil {
			return failedResult("upload.finish", "invalid_arguments", err.Error())
		}
	}
	id, err := parseTypedHandle(args.Handle, "upload")
	if err != nil {
		return failedResult("upload.finish", "invalid_arguments", err.Error())
	}
	dataPath := uploadDataPath(d.uploadsDir, id)
	file, err := os.OpenFile(dataPath, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("upload.finish", "not_found", "upload not found")
	}
	if err != nil {
		return failedResult("upload.finish", "upload_failed", err.Error())
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return failedResult("upload.finish", "upload_failed", err.Error())
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	metadata, err := loadUploadMetadata(d.uploadsDir, id)
	if err != nil {
		return failedResult("upload.finish", "upload_failed", err.Error())
	}
	if err := verifyUpload(file, metadata); err != nil {
		return failedResult("upload.finish", "verification_failed", err.Error())
	}

	if args.Destination != nil {
		destination := filepath.Clean(*args.Destination)
		bytesWritten, err := moveFileToAtomicDestination(dataPath, destination, args.Overwrite)
		if errors.Is(err, os.ErrExist) || errors.Is(err, syscall.EEXIST) {
			return failedResult("upload.finish", "destination_exists", "destination exists")
		}
		if err != nil {
			return failedResult("upload.finish", "upload_failed", err.Error())
		}
		if err := os.RemoveAll(uploadDirectory(d.uploadsDir, id)); err != nil {
			return failedResult("upload.finish", "upload_failed", err.Error())
		}
		result := newResult("upload.finish")
		result.OK = true
		result.Result = map[string]any{
			"upload_handle": args.Handle,
			"destination":   destination,
			"bytes_written": bytesWritten,
		}
		return result
	}

	name := args.Name
	if name == "" {
		name = metadata.Filename
	}
	artifactMetadata, descriptor, err := d.createFileArtifact(ctx, dataPath, name, args.MediaType)
	if err != nil {
		return failedResult("upload.finish", "artifact_failed", err.Error())
	}
	if err := os.RemoveAll(uploadDirectory(d.uploadsDir, id)); err != nil {
		return failedResult("upload.finish", "upload_failed", err.Error())
	}
	artifactHandle := artifactMetadata.Handle
	result := newResult("upload.finish")
	result.OK = true
	result.Handle = &artifactHandle
	result.Result = map[string]any{
		"upload_handle": args.Handle,
		"handle":        artifactHandle,
		"metadata":      artifactMetadata,
		"uri":           artifactMetadata.URI,
	}
	result.Resources = []any{descriptor}
	return result
}

func (d *Dispatcher) uploadAbort(_ context.Context, raw map[string]any) Result {
	var args uploadAbortArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("upload.abort", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("upload.abort", "invalid_arguments", "handle is required")
	}
	id, err := parseTypedHandle(args.Handle, "upload")
	if err != nil {
		return failedResult("upload.abort", "invalid_arguments", err.Error())
	}
	file, err := os.OpenFile(uploadDataPath(d.uploadsDir, id), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("upload.abort", "not_found", "upload not found")
	}
	if err != nil {
		return failedResult("upload.abort", "upload_failed", err.Error())
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return failedResult("upload.abort", "upload_failed", err.Error())
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if _, err := loadUploadMetadata(d.uploadsDir, id); err != nil {
		return failedResult("upload.abort", "upload_failed", err.Error())
	}
	if err := os.RemoveAll(uploadDirectory(d.uploadsDir, id)); err != nil {
		return failedResult("upload.abort", "upload_failed", err.Error())
	}
	result := newResult("upload.abort")
	result.OK = true
	result.Result = map[string]any{"handle": args.Handle, "aborted": true}
	return result
}
