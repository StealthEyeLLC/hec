package hec

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func newTransferDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	dispatcher := NewDispatcher()
	root := t.TempDir()
	dispatcher.uploadsDir = filepath.Join(root, "uploads")
	dispatcher.artifactsDir = filepath.Join(root, "artifacts")
	return dispatcher
}

func beginUploadForTest(t *testing.T, dispatcher *Dispatcher, args map[string]any) string {
	t.Helper()
	result := dispatchForTest(t, dispatcher, "upload.begin", args)
	requireOK(t, result)
	if result.Handle == nil {
		t.Fatal("upload.begin returned no handle")
	}
	return *result.Handle
}

func writeUploadChunkForTest(t *testing.T, dispatcher *Dispatcher, handle string, offset int64, data []byte) Result {
	t.Helper()
	return dispatchForTest(t, dispatcher, "upload.chunk", map[string]any{
		"handle":      handle,
		"offset":      offset,
		"data_base64": base64.StdEncoding.EncodeToString(data),
	})
}

func TestUploadCreationChunkOverwriteGapAndRestartRecovery(t *testing.T) {
	dispatcher := newTransferDispatcher(t)
	handle := beginUploadForTest(t, dispatcher, map[string]any{"filename": "resume.bin"})
	id, err := parseTypedHandle(handle, "upload")
	if err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(uploadDirectory(dispatcher.uploadsDir, id))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("upload directory mode = %o", dirInfo.Mode().Perm())
	}
	for _, name := range []string{"data", "metadata.json"} {
		info, err := os.Stat(filepath.Join(uploadDirectory(dispatcher.uploadsDir, id), name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode = %o", name, info.Mode().Perm())
		}
	}

	first := writeUploadChunkForTest(t, dispatcher, handle, 0, []byte("abc"))
	requireOK(t, first)
	retry := writeUploadChunkForTest(t, dispatcher, handle, 0, []byte("aBc"))
	requireOK(t, retry)
	gap := writeUploadChunkForTest(t, dispatcher, handle, 4, []byte("x"))
	if gap.OK || gap.Error == nil || gap.Error.Code != "invalid_offset" {
		t.Fatalf("gap result = %#v", gap)
	}

	restarted := NewDispatcher()
	restarted.uploadsDir = dispatcher.uploadsDir
	restarted.artifactsDir = dispatcher.artifactsDir
	continued := writeUploadChunkForTest(t, restarted, handle, 3, []byte("def"))
	requireOK(t, continued)
	payload, err := os.ReadFile(uploadDataPath(dispatcher.uploadsDir, id))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, []byte("aBcdef")) {
		t.Fatalf("upload payload = %q", payload)
	}
	metadata, err := loadUploadMetadata(dispatcher.uploadsDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CurrentBytes != 6 {
		t.Fatalf("current bytes = %d", metadata.CurrentBytes)
	}
	aborted := dispatchForTest(t, restarted, "upload.abort", map[string]any{"handle": handle})
	requireOK(t, aborted)
	if _, err := os.Stat(uploadDirectory(dispatcher.uploadsDir, id)); !os.IsNotExist(err) {
		t.Fatalf("upload remains after abort: %v", err)
	}
}

func TestUploadSizeAndSHA256VerificationAndDestinationCompletion(t *testing.T) {
	dispatcher := newTransferDispatcher(t)
	payload := []byte("abcdef")
	digest := sha256.Sum256(payload)
	handle := beginUploadForTest(t, dispatcher, map[string]any{
		"filename": "verified.bin",
		"size":     len(payload),
		"sha256":   hex.EncodeToString(digest[:]),
	})
	requireOK(t, writeUploadChunkForTest(t, dispatcher, handle, 0, payload[:3]))
	destination := filepath.Join(t.TempDir(), "verified.bin")
	incomplete := dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": handle, "destination": destination,
	})
	if incomplete.OK || incomplete.Error == nil || incomplete.Error.Code != "verification_failed" {
		t.Fatalf("incomplete finish = %#v", incomplete)
	}
	id, _ := parseTypedHandle(handle, "upload")
	if _, err := os.Stat(uploadDirectory(dispatcher.uploadsDir, id)); err != nil {
		t.Fatalf("failed verification removed upload: %v", err)
	}
	requireOK(t, writeUploadChunkForTest(t, dispatcher, handle, 3, payload[3:]))
	finished := dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": handle, "destination": destination,
	})
	requireOK(t, finished)
	actual, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("destination = %q, %v", actual, err)
	}
	if _, err := os.Stat(uploadDirectory(dispatcher.uploadsDir, id)); !os.IsNotExist(err) {
		t.Fatalf("completed upload remains: %v", err)
	}

	shaPayload := []byte("xyz")
	shaDigest := sha256.Sum256(shaPayload)
	shaHandle := beginUploadForTest(t, dispatcher, map[string]any{
		"filename": "sha.bin",
		"size":     len(shaPayload),
		"sha256":   hex.EncodeToString(shaDigest[:]),
	})
	requireOK(t, writeUploadChunkForTest(t, dispatcher, shaHandle, 0, []byte("bad")))
	shaDestination := filepath.Join(t.TempDir(), "sha.bin")
	badDigest := dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": shaHandle, "destination": shaDestination,
	})
	if badDigest.OK || badDigest.Error == nil || badDigest.Error.Code != "verification_failed" {
		t.Fatalf("bad digest finish = %#v", badDigest)
	}
	requireOK(t, writeUploadChunkForTest(t, dispatcher, shaHandle, 0, shaPayload))
	requireOK(t, dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": shaHandle, "destination": shaDestination,
	}))
	actual, err = os.ReadFile(shaDestination)
	if err != nil || !bytes.Equal(actual, shaPayload) {
		t.Fatalf("sha destination = %q, %v", actual, err)
	}
}

func TestUploadCrossFilesystemCompletionWhenAvailable(t *testing.T) {
	sharedMemory := "/dev/shm"
	sharedInfo, err := os.Stat(sharedMemory)
	if err != nil || !sharedInfo.IsDir() {
		t.Skip("/dev/shm is unavailable")
	}
	dispatcher := newTransferDispatcher(t)
	if err := ensureStateRoot(dispatcher.uploadsDir); err != nil {
		t.Fatal(err)
	}
	uploadInfo, err := os.Stat(dispatcher.uploadsDir)
	if err != nil {
		t.Fatal(err)
	}
	if uploadInfo.Sys().(*syscall.Stat_t).Dev == sharedInfo.Sys().(*syscall.Stat_t).Dev {
		t.Skip("test paths are on the same filesystem")
	}
	id, err := secureID()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(sharedMemory, "hec-upload-test-"+id)
	defer os.Remove(destination)
	payload := []byte("cross-filesystem")
	handle := beginUploadForTest(t, dispatcher, map[string]any{"filename": "cross.bin", "size": len(payload)})
	requireOK(t, writeUploadChunkForTest(t, dispatcher, handle, 0, payload))
	result := dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": handle, "destination": destination,
	})
	requireOK(t, result)
	actual, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("cross-filesystem destination = %q, %v", actual, err)
	}
}
