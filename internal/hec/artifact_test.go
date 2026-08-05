package hec

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func artifactHandleFromResult(t *testing.T, result Result) string {
	t.Helper()
	requireOK(t, result)
	if result.Handle == nil {
		t.Fatal("artifact operation returned no handle")
	}
	return *result.Handle
}

func TestFileArtifactMetadataRangesMaterializationListingAndDeletion(t *testing.T) {
	dispatcher := newTransferDispatcher(t)
	source := filepath.Join(t.TempDir(), "binary.bin")
	payload := []byte{0xff, 0x00, 0x41, 0xfe, 0x42, 0x43}
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	returned := dispatchForTest(t, dispatcher, "artifact.return", map[string]any{
		"path": source, "name": "binary.bin", "media_type": "application/octet-stream",
	})
	handle := artifactHandleFromResult(t, returned)
	if len(returned.Resources) != 1 {
		t.Fatalf("resources = %#v", returned.Resources)
	}
	descriptor, ok := returned.Resources[0].(ResourceDescriptor)
	if !ok || descriptor.Name != "binary.bin" || descriptor.MediaType != "application/octet-stream" || descriptor.Size != int64(len(payload)) {
		t.Fatalf("descriptor = %#v", returned.Resources[0])
	}
	id, err := parseTypedHandle(handle, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := loadArtifactMetadata(dispatcher.artifactsDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Filename != "binary.bin" || metadata.SourceKind != "file" || metadata.URI != "hec://artifact/"+id || metadata.Size != int64(len(payload)) {
		t.Fatalf("metadata = %#v", metadata)
	}

	if err := os.WriteFile(source, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(artifactPayloadPath(dispatcher.artifactsDir, id))
	if err != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("artifact changed with source: %x, %v", stored, err)
	}
	stat := dispatchForTest(t, dispatcher, "artifact.stat", map[string]any{"handle": handle})
	requireOK(t, stat)
	storedMetadata, ok := stat.Result["metadata"].(ArtifactMetadata)
	if !ok || storedMetadata.Handle != handle {
		t.Fatalf("artifact.stat = %#v", stat.Result)
	}

	first := dispatchForTest(t, dispatcher, "artifact.read", map[string]any{"handle": handle, "offset": 0, "limit": 3})
	requireOK(t, first)
	if first.Result["encoding"] != "base64" || first.Result["next_offset"] != int64(3) || first.Result["eof"] != false {
		t.Fatalf("first range = %#v", first.Result)
	}
	firstBytes, err := base64.StdEncoding.DecodeString(first.Result["data"].(string))
	if err != nil || !bytes.Equal(firstBytes, payload[:3]) {
		t.Fatalf("first bytes = %x, %v", firstBytes, err)
	}
	second := dispatchForTest(t, dispatcher, "artifact.read", map[string]any{"handle": handle, "offset": 3, "limit": 10})
	requireOK(t, second)
	secondBytes, err := base64.StdEncoding.DecodeString(second.Result["data"].(string))
	if err != nil || !bytes.Equal(secondBytes, payload[3:]) || second.Result["eof"] != true {
		t.Fatalf("second range = %x, %v, %#v", secondBytes, err, second.Result)
	}

	materialized := filepath.Join(t.TempDir(), "copy.bin")
	materialize := dispatchForTest(t, dispatcher, "artifact.materialize", map[string]any{
		"handle": handle, "destination": materialized,
	})
	requireOK(t, materialize)
	actual, err := os.ReadFile(materialized)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("materialized = %x, %v", actual, err)
	}

	otherSource := filepath.Join(t.TempDir(), "other.txt")
	if err := os.WriteFile(otherSource, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	other := artifactHandleFromResult(t, dispatchForTest(t, dispatcher, "artifact.return", map[string]any{"path": otherSource}))
	listed := dispatchForTest(t, dispatcher, "artifact.list", map[string]any{})
	requireOK(t, listed)
	artifacts, ok := listed.Result["artifacts"].([]ArtifactMetadata)
	if !ok || len(artifacts) != 2 {
		t.Fatalf("artifact.list = %#v", listed.Result)
	}
	handles := []string{artifacts[0].Handle, artifacts[1].Handle}
	if !sort.StringsAreSorted(handles) {
		t.Fatalf("artifact order = %#v", handles)
	}

	deleted := dispatchForTest(t, dispatcher, "artifact.delete", map[string]any{"handle": handle})
	requireOK(t, deleted)
	if _, err := os.Stat(artifactDirectory(dispatcher.artifactsDir, id)); !os.IsNotExist(err) {
		t.Fatalf("deleted artifact remains: %v", err)
	}
	otherID, _ := parseTypedHandle(other, "artifact")
	if _, err := os.Stat(artifactDirectory(dispatcher.artifactsDir, otherID)); err != nil {
		t.Fatalf("unrelated artifact removed: %v", err)
	}
	unknown := dispatchForTest(t, dispatcher, "artifact.stat", map[string]any{"handle": "artifact:ffffffffffffffffffffffffffffffff"})
	if unknown.OK || unknown.Error == nil || unknown.Error.Code != "not_found" {
		t.Fatalf("unknown artifact = %#v", unknown)
	}
}

func TestUploadPromotionUsesArtifactStorage(t *testing.T) {
	dispatcher := newTransferDispatcher(t)
	payload := []byte("promoted artifact")
	handle := beginUploadForTest(t, dispatcher, map[string]any{"filename": "promoted.txt", "size": len(payload)})
	requireOK(t, writeUploadChunkForTest(t, dispatcher, handle, 0, payload[:5]))
	requireOK(t, writeUploadChunkForTest(t, dispatcher, handle, 5, payload[5:]))
	finished := dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": handle, "artifact": true, "media_type": "text/plain",
	})
	artifactHandle := artifactHandleFromResult(t, finished)
	if len(finished.Resources) != 1 || finished.Result["uri"] == "" {
		t.Fatalf("upload artifact result = %#v resources=%#v", finished.Result, finished.Resources)
	}
	artifactID, err := parseTypedHandle(artifactHandle, "artifact")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(artifactPayloadPath(dispatcher.artifactsDir, artifactID))
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatalf("promoted payload = %q, %v", actual, err)
	}
	uploadID, _ := parseTypedHandle(handle, "upload")
	if _, err := os.Stat(uploadDirectory(dispatcher.uploadsDir, uploadID)); !os.IsNotExist(err) {
		t.Fatalf("completed upload remains: %v", err)
	}
	metadata, err := loadArtifactMetadata(dispatcher.artifactsDir, artifactID)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.SourceUploadHandle != handle {
		t.Fatalf("source upload handle = %q, want %q", metadata.SourceUploadHandle, handle)
	}
	replayed := dispatchForTest(t, dispatcher, "upload.finish", map[string]any{
		"handle": handle, "artifact": true, "media_type": "text/plain",
	})
	replayedHandle := artifactHandleFromResult(t, replayed)
	if replayedHandle != artifactHandle || len(replayed.Resources) != 1 {
		t.Fatalf("replayed finish = %#v resources=%#v", replayed.Result, replayed.Resources)
	}
	listed := dispatchForTest(t, dispatcher, "artifact.list", map[string]any{})
	requireOK(t, listed)
	artifacts, ok := listed.Result["artifacts"].([]ArtifactMetadata)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("replayed finish created duplicate artifacts: %#v", listed.Result)
	}
}

func TestDeterministicDirectoryArtifact(t *testing.T) {
	dispatcher := newTransferDispatcher(t)
	parent := t.TempDir()
	source := filepath.Join(parent, "tree")
	if err := os.MkdirAll(filepath.Join(source, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "sub", "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}

	first := artifactHandleFromResult(t, dispatchForTest(t, dispatcher, "artifact.return", map[string]any{"path": source}))
	second := artifactHandleFromResult(t, dispatchForTest(t, dispatcher, "artifact.return", map[string]any{"path": source}))
	firstID, _ := parseTypedHandle(first, "artifact")
	secondID, _ := parseTypedHandle(second, "artifact")
	firstPayload, err := os.ReadFile(artifactPayloadPath(dispatcher.artifactsDir, firstID))
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := os.ReadFile(artifactPayloadPath(dispatcher.artifactsDir, secondID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstPayload, secondPayload) {
		t.Fatal("directory archive bytes are not deterministic")
	}
	metadata, err := loadArtifactMetadata(dispatcher.artifactsDir, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.SourceKind != "directory" || metadata.Filename != "tree.tar.zst" || metadata.MediaType != "application/zstd" {
		t.Fatalf("directory metadata = %#v", metadata)
	}

	payloadPath := artifactPayloadPath(dispatcher.artifactsDir, firstID)
	listCommand := exec.Command(dispatcher.tarPath, "--use-compress-program="+dispatcher.zstdPath, "-tf", payloadPath)
	listing, err := listCommand.Output()
	if err != nil {
		t.Fatalf("list archive: %v", err)
	}
	listed := string(listing)
	for _, path := range []string{"tree/", "tree/a.txt", "tree/link", "tree/sub/", "tree/sub/run.sh"} {
		if !strings.Contains(listed, path) {
			t.Fatalf("archive listing missing %q: %s", path, listed)
		}
	}
	contentCommand := exec.Command(dispatcher.tarPath, "--use-compress-program="+dispatcher.zstdPath, "-xOf", payloadPath, "tree/a.txt")
	content, err := contentCommand.Output()
	if err != nil || string(content) != "alpha\n" {
		t.Fatalf("archive content = %q, %v", content, err)
	}
}
