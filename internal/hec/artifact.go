package hec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ArtifactMetadata struct {
	ID                 string `json:"id"`
	Handle             string `json:"handle"`
	Filename           string `json:"filename"`
	MediaType          string `json:"media_type"`
	SourceKind         string `json:"source_kind"`
	SourceUploadHandle string `json:"source_upload_handle,omitempty"`
	Size               int64  `json:"size"`
	URI                string `json:"uri"`
}

type artifactReturnArgs struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
}

type artifactHandleArgs struct {
	Handle string `json:"handle"`
}

type artifactReadArgs struct {
	Handle string `json:"handle"`
	Offset *int64 `json:"offset"`
	Limit  *int64 `json:"limit"`
}

type artifactMaterializeArgs struct {
	Handle      string `json:"handle"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

type artifactListArgs struct {
	Offset *int64 `json:"offset"`
	Limit  *int64 `json:"limit"`
}

func artifactDirectory(root, id string) string {
	return filepath.Join(root, id)
}

func artifactMetadataPath(root, id string) string {
	return filepath.Join(artifactDirectory(root, id), "metadata.json")
}

func artifactPayloadPath(root, id string) string {
	return filepath.Join(artifactDirectory(root, id), "payload")
}

func loadArtifactMetadata(root, id string) (ArtifactMetadata, error) {
	var metadata ArtifactMetadata
	data, err := os.ReadFile(artifactMetadataPath(root, id))
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, err
	}
	if metadata.ID != id || metadata.Handle != "artifact:"+id || metadata.URI != "hec://artifact/"+id {
		return metadata, errors.New("artifact metadata does not match handle")
	}
	return metadata, nil
}

func resourceDescriptorForArtifact(metadata ArtifactMetadata) ResourceDescriptor {
	return ResourceDescriptor{
		URI:       metadata.URI,
		Name:      metadata.Filename,
		MediaType: metadata.MediaType,
		Size:      metadata.Size,
	}
}

func findArtifactByUploadHandle(root, uploadHandle string) (ArtifactMetadata, bool, error) {
	var empty ArtifactMetadata
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		if _, err := parseTypedHandle("artifact:"+entry.Name(), "artifact"); err != nil {
			continue
		}
		metadata, err := loadArtifactMetadata(root, entry.Name())
		if err != nil {
			return empty, false, err
		}
		if metadata.SourceUploadHandle == uploadHandle {
			return metadata, true, nil
		}
	}
	return empty, false, nil
}

func artifactMediaType(filename, supplied string) string {
	if supplied != "" {
		return supplied
	}
	if detected := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); detected != "" {
		return detected
	}
	return "application/octet-stream"
}

func newArtifactDirectory(root string) (id, tempDir, finalDir string, err error) {
	if err := ensureStateRoot(root); err != nil {
		return "", "", "", err
	}
	for attempts := 0; attempts < 4; attempts++ {
		id, err = secureID()
		if err != nil {
			return "", "", "", err
		}
		tempDir = filepath.Join(root, ".tmp-"+id)
		finalDir = artifactDirectory(root, id)
		if err := os.Mkdir(tempDir, 0700); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return "", "", "", err
		}
		return id, tempDir, finalDir, nil
	}
	return "", "", "", errors.New("could not allocate artifact handle")
}

func completeArtifact(tempDir, finalDir, id, filename, mediaType, sourceKind, sourceUploadHandle string) (ArtifactMetadata, ResourceDescriptor, error) {
	var metadata ArtifactMetadata
	var descriptor ResourceDescriptor
	payloadInfo, err := os.Stat(filepath.Join(tempDir, "payload"))
	if err != nil {
		return metadata, descriptor, err
	}
	metadata = ArtifactMetadata{
		ID:                 id,
		Handle:             "artifact:" + id,
		Filename:           filename,
		MediaType:          mediaType,
		SourceKind:         sourceKind,
		SourceUploadHandle: sourceUploadHandle,
		Size:               payloadInfo.Size(),
		URI:                "hec://artifact/" + id,
	}
	if err := writeJSONAtomic(filepath.Join(tempDir, "metadata.json"), metadata, 0600); err != nil {
		return ArtifactMetadata{}, descriptor, err
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		return ArtifactMetadata{}, descriptor, err
	}
	descriptor = resourceDescriptorForArtifact(metadata)
	return metadata, descriptor, nil
}

func (d *Dispatcher) createFileArtifact(_ context.Context, source, name, mediaType, sourceUploadHandle string) (ArtifactMetadata, ResourceDescriptor, error) {
	var emptyMetadata ArtifactMetadata
	var emptyDescriptor ResourceDescriptor
	if err := validateBasename(name, "name"); err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	if !info.Mode().IsRegular() {
		return emptyMetadata, emptyDescriptor, errors.New("source is not a regular file")
	}
	id, tempDir, finalDir, err := newArtifactDirectory(d.artifactsDir)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(tempDir)
		}
	}()

	input, err := os.Open(source)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	defer input.Close()
	payload, err := os.OpenFile(filepath.Join(tempDir, "payload"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	if _, err := payload.ReadFrom(input); err != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, err
	}
	if err := payload.Sync(); err != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, err
	}
	if err := payload.Close(); err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	metadata, descriptor, err := completeArtifact(tempDir, finalDir, id, name, artifactMediaType(name, mediaType), "file", sourceUploadHandle)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	removeTemp = false
	return metadata, descriptor, nil
}

func (d *Dispatcher) createDirectoryArtifact(ctx context.Context, source, name, mediaType string) (ArtifactMetadata, ResourceDescriptor, error) {
	var emptyMetadata ArtifactMetadata
	var emptyDescriptor ResourceDescriptor
	if err := validateBasename(name, "name"); err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	if !info.IsDir() {
		return emptyMetadata, emptyDescriptor, errors.New("source is not a directory")
	}
	id, tempDir, finalDir, err := newArtifactDirectory(d.artifactsDir)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(tempDir)
		}
	}()
	payload, err := os.OpenFile(filepath.Join(tempDir, "payload"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	tarArgs := []string{
		"--sort=name",
		"--format=gnu",
		"--mtime=@0",
		"--owner=0",
		"--group=0",
		"--numeric-owner",
		"-cf", "-",
		"-C", filepath.Dir(source),
		"--", filepath.Base(source),
	}
	tarCommand := exec.CommandContext(ctx, d.tarPath, tarArgs...)
	tarCommand.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	tarOutput, err := tarCommand.StdoutPipe()
	if err != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, err
	}
	var tarError bytes.Buffer
	tarCommand.Stderr = &tarError
	zstdCommand := exec.CommandContext(ctx, d.zstdPath, "-q", "-c", "-T1")
	zstdCommand.Stdin = tarOutput
	zstdCommand.Stdout = payload
	var zstdError bytes.Buffer
	zstdCommand.Stderr = &zstdError
	if err := zstdCommand.Start(); err != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, err
	}
	if err := tarCommand.Start(); err != nil {
		_ = zstdCommand.Process.Kill()
		_ = zstdCommand.Wait()
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, err
	}
	tarWaitErr := tarCommand.Wait()
	zstdWaitErr := zstdCommand.Wait()
	if tarWaitErr != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, fmt.Errorf("tar failed: %s", strings.TrimSpace(tarError.String()))
	}
	if zstdWaitErr != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, fmt.Errorf("zstd failed: %s", strings.TrimSpace(zstdError.String()))
	}
	if err := payload.Sync(); err != nil {
		_ = payload.Close()
		return emptyMetadata, emptyDescriptor, err
	}
	if err := payload.Close(); err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	if mediaType == "" {
		mediaType = "application/zstd"
	}
	metadata, descriptor, err := completeArtifact(tempDir, finalDir, id, name, mediaType, "directory", "")
	if err != nil {
		return emptyMetadata, emptyDescriptor, err
	}
	removeTemp = false
	return metadata, descriptor, nil
}

func (d *Dispatcher) artifactReturn(ctx context.Context, raw map[string]any) Result {
	var args artifactReturnArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("artifact.return", "invalid_arguments", err.Error())
	}
	path, err := resolveFilesystemPath(args.Path)
	if err != nil {
		return failedResult("artifact.return", "invalid_arguments", err.Error())
	}
	if args.Name != "" {
		if err := validateBasename(args.Name, "name"); err != nil {
			return failedResult("artifact.return", "invalid_arguments", err.Error())
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("artifact.return", "not_found", "source path not found")
	}
	if err != nil {
		return failedResult("artifact.return", "artifact_failed", err.Error())
	}
	name := args.Name
	var metadata ArtifactMetadata
	var descriptor ResourceDescriptor
	switch {
	case info.Mode().IsRegular():
		if name == "" {
			name = filepath.Base(path)
		}
		metadata, descriptor, err = d.createFileArtifact(ctx, path, name, args.MediaType, "")
	case info.IsDir():
		if name == "" {
			name = filepath.Base(path) + ".tar.zst"
		}
		metadata, descriptor, err = d.createDirectoryArtifact(ctx, path, name, args.MediaType)
	default:
		return failedResult("artifact.return", "invalid_source", "source path must be a regular file or directory")
	}
	if err != nil {
		return failedResult("artifact.return", "artifact_failed", err.Error())
	}
	handle := metadata.Handle
	result := newResult("artifact.return")
	result.OK = true
	result.Handle = &handle
	result.Result = map[string]any{
		"handle":   handle,
		"metadata": metadata,
		"uri":      metadata.URI,
	}
	result.Resources = []any{descriptor}
	return result
}

func (d *Dispatcher) artifactStat(_ context.Context, raw map[string]any) Result {
	var args artifactHandleArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("artifact.stat", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("artifact.stat", "invalid_arguments", "handle is required")
	}
	id, err := parseTypedHandle(args.Handle, "artifact")
	if err != nil {
		return failedResult("artifact.stat", "invalid_arguments", err.Error())
	}
	metadata, err := loadArtifactMetadata(d.artifactsDir, id)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("artifact.stat", "not_found", "artifact not found")
	}
	if err != nil {
		return failedResult("artifact.stat", "artifact_failed", err.Error())
	}
	result := newResult("artifact.stat")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{"metadata": metadata}
	return result
}

func (d *Dispatcher) artifactRead(_ context.Context, raw map[string]any) Result {
	var args artifactReadArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("artifact.read", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("artifact.read", "invalid_arguments", "handle is required")
	}
	id, err := parseTypedHandle(args.Handle, "artifact")
	if err != nil {
		return failedResult("artifact.read", "invalid_arguments", err.Error())
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
		return failedResult("artifact.read", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if limit <= 0 || limit > MaximumRangeLimit {
		return failedResult("artifact.read", "invalid_arguments", "limit must be between 1 and 1048576")
	}
	metadata, err := loadArtifactMetadata(d.artifactsDir, id)
	if errors.Is(err, os.ErrNotExist) {
		return failedResult("artifact.read", "not_found", "artifact not found")
	}
	if err != nil {
		return failedResult("artifact.read", "artifact_failed", err.Error())
	}
	file, err := os.Open(artifactPayloadPath(d.artifactsDir, id))
	if err != nil {
		return failedResult("artifact.read", "artifact_failed", err.Error())
	}
	defer file.Close()
	data, next, total, eof, err := readRange(file, offset, limit)
	if err != nil {
		return failedResult("artifact.read", "artifact_failed", err.Error())
	}
	encoded, encoding := encodeData(data)
	result := newResult("artifact.read")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":      args.Handle,
		"filename":    metadata.Filename,
		"offset":      offset,
		"next_offset": next,
		"total_bytes": total,
		"eof":         eof,
		"encoding":    encoding,
		"data":        encoded,
	}
	return result
}

func (d *Dispatcher) artifactMaterialize(_ context.Context, raw map[string]any) Result {
	var args artifactMaterializeArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("artifact.materialize", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("artifact.materialize", "invalid_arguments", "handle is required")
	}
	if !filepath.IsAbs(args.Destination) {
		return failedResult("artifact.materialize", "invalid_arguments", "destination must be absolute")
	}
	id, err := parseTypedHandle(args.Handle, "artifact")
	if err != nil {
		return failedResult("artifact.materialize", "invalid_arguments", err.Error())
	}
	if _, err := loadArtifactMetadata(d.artifactsDir, id); errors.Is(err, os.ErrNotExist) {
		return failedResult("artifact.materialize", "not_found", "artifact not found")
	} else if err != nil {
		return failedResult("artifact.materialize", "artifact_failed", err.Error())
	}
	destination := filepath.Clean(args.Destination)
	written, err := copyFileToAtomicDestination(artifactPayloadPath(d.artifactsDir, id), destination, args.Overwrite)
	if errors.Is(err, os.ErrExist) {
		return failedResult("artifact.materialize", "destination_exists", "destination exists")
	}
	if err != nil {
		return failedResult("artifact.materialize", "artifact_failed", err.Error())
	}
	result := newResult("artifact.materialize")
	result.OK = true
	result.Handle = &args.Handle
	result.Result = map[string]any{
		"handle":        args.Handle,
		"destination":   destination,
		"bytes_written": written,
	}
	return result
}

func (d *Dispatcher) artifactList(_ context.Context, raw map[string]any) Result {
	var args artifactListArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("artifact.list", "invalid_arguments", err.Error())
	}
	offset := int64(0)
	if args.Offset != nil {
		offset = *args.Offset
	}
	limit := int64(100)
	if args.Limit != nil {
		limit = *args.Limit
	}
	if offset < 0 {
		return failedResult("artifact.list", "invalid_arguments", "offset must be greater than or equal to zero")
	}
	if limit <= 0 || limit > 1000 {
		return failedResult("artifact.list", "invalid_arguments", "limit must be between 1 and 1000")
	}
	if err := ensureStateRoot(d.artifactsDir); err != nil {
		return failedResult("artifact.list", "artifact_failed", err.Error())
	}
	entries, err := os.ReadDir(d.artifactsDir)
	if err != nil {
		return failedResult("artifact.list", "artifact_failed", err.Error())
	}
	artifacts := make([]ArtifactMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		if _, err := parseTypedHandle("artifact:"+entry.Name(), "artifact"); err != nil {
			continue
		}
		metadata, err := loadArtifactMetadata(d.artifactsDir, entry.Name())
		if err != nil {
			return failedResult("artifact.list", "artifact_failed", err.Error())
		}
		artifacts = append(artifacts, metadata)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Handle < artifacts[j].Handle })
	total := int64(len(artifacts))
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	listed := artifacts[start:end]
	result := newResult("artifact.list")
	result.OK = true
	result.Result = map[string]any{
		"offset":          offset,
		"next_offset":     end,
		"total_artifacts": total,
		"eof":             end >= total,
		"artifacts":       listed,
	}
	return result
}

func (d *Dispatcher) artifactDelete(_ context.Context, raw map[string]any) Result {
	var args artifactHandleArgs
	if err := decodeOperationArgs(raw, &args); err != nil {
		return failedResult("artifact.delete", "invalid_arguments", err.Error())
	}
	if args.Handle == "" {
		return failedResult("artifact.delete", "invalid_arguments", "handle is required")
	}
	id, err := parseTypedHandle(args.Handle, "artifact")
	if err != nil {
		return failedResult("artifact.delete", "invalid_arguments", err.Error())
	}
	if _, err := loadArtifactMetadata(d.artifactsDir, id); errors.Is(err, os.ErrNotExist) {
		return failedResult("artifact.delete", "not_found", "artifact not found")
	} else if err != nil {
		return failedResult("artifact.delete", "artifact_failed", err.Error())
	}
	if err := os.RemoveAll(artifactDirectory(d.artifactsDir, id)); err != nil {
		return failedResult("artifact.delete", "artifact_failed", err.Error())
	}
	result := newResult("artifact.delete")
	result.OK = true
	result.Result = map[string]any{"handle": args.Handle, "deleted": true}
	return result
}

func (d *Dispatcher) readArtifactResource(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := request.Params.URI
	const prefix = "hec://artifact/"
	if !strings.HasPrefix(uri, prefix) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	id := strings.TrimPrefix(uri, prefix)
	if parsed, err := parseTypedHandle("artifact:"+id, "artifact"); err != nil || uri != prefix+parsed {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	metadata, err := loadArtifactMetadata(d.artifactsDir, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if err != nil {
		return nil, fmt.Errorf("read artifact resource: %w", err)
	}
	payload, err := os.ReadFile(artifactPayloadPath(d.artifactsDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if err != nil {
		return nil, fmt.Errorf("read artifact resource: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: metadata.MediaType,
			Blob:     payload,
			Meta:     mcp.Meta{"name": metadata.Filename},
		}},
	}, nil
}
