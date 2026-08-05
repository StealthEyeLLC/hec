package hec

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func dispatchForTest(t *testing.T, dispatcher *Dispatcher, operation string, args map[string]any) Result {
	t.Helper()
	result := dispatcher.Dispatch(context.Background(), CallRequest{Operation: operation, Args: args})
	return result
}

func requireOK(t *testing.T, result Result) {
	t.Helper()
	if !result.OK {
		t.Fatalf("operation %s failed: %#v", result.Operation, result.Error)
	}
}

func TestFileMetadataAndDirectoryPagination(t *testing.T) {
	dispatcher := NewDispatcher()
	dir := t.TempDir()
	for name, data := range map[string]string{
		"b.txt":   "bb",
		".hidden": "h",
		"a.txt":   "a",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	stat := dispatchForTest(t, dispatcher, "file.stat", map[string]any{"path": filepath.Join(dir, "link")})
	requireOK(t, stat)
	if stat.Result["type"] != "symlink" || stat.Result["symlink_target"] != "a.txt" {
		t.Fatalf("symlink stat = %#v", stat.Result)
	}
	for _, key := range []string{"path", "name", "type", "size", "mode", "uid", "gid", "modified_at"} {
		if _, ok := stat.Result[key]; !ok {
			t.Fatalf("stat missing %q: %#v", key, stat.Result)
		}
	}

	first := dispatchForTest(t, dispatcher, "file.list", map[string]any{"path": dir, "offset": 0, "limit": 2})
	requireOK(t, first)
	entries, ok := first.Result["entries"].([]map[string]any)
	if !ok {
		t.Fatalf("entries type = %T", first.Result["entries"])
	}
	gotNames := []string{entries[0]["name"].(string), entries[1]["name"].(string)}
	if !reflect.DeepEqual(gotNames, []string{".hidden", "a.txt"}) {
		t.Fatalf("first names = %#v", gotNames)
	}
	if first.Result["next_offset"] != int64(2) || first.Result["eof"] != false || first.Result["total_entries"] != int64(4) {
		t.Fatalf("first page = %#v", first.Result)
	}
	second := dispatchForTest(t, dispatcher, "file.list", map[string]any{"path": dir, "offset": 2, "limit": 10})
	requireOK(t, second)
	entries = second.Result["entries"].([]map[string]any)
	gotNames = []string{entries[0]["name"].(string), entries[1]["name"].(string)}
	if !reflect.DeepEqual(gotNames, []string{"b.txt", "link"}) || second.Result["eof"] != true {
		t.Fatalf("second page = %#v", second.Result)
	}
}

func TestAtomicFileReplacementAndBinaryWrite(t *testing.T) {
	dispatcher := NewDispatcher()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(path, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeInode := beforeInfo.Sys().(*syscall.Stat_t).Ino

	invalid := dispatchForTest(t, dispatcher, "file.write", map[string]any{"path": path, "content_base64": "%%%"})
	if invalid.OK || invalid.Error == nil || invalid.Error.Code != "invalid_arguments" {
		t.Fatalf("invalid base64 result = %#v", invalid)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "old" {
		t.Fatalf("destination changed after invalid base64: %q %v", data, err)
	}

	binary := []byte{0xff, 0x00, 0xfe, 0x41}
	written := dispatchForTest(t, dispatcher, "file.write", map[string]any{
		"path":           path,
		"content_base64": base64.StdEncoding.EncodeToString(binary),
	})
	requireOK(t, written)
	if written.Result["bytes_written"] != len(binary) || written.Result["replaced"] != true {
		t.Fatalf("write result = %#v", written.Result)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Sys().(*syscall.Stat_t).Ino == beforeInode {
		t.Fatal("atomic replacement did not replace the destination inode")
	}
	if afterInfo.Mode().Perm() != 0640 {
		t.Fatalf("mode = %o, want 0640", afterInfo.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, binary) {
		t.Fatalf("binary bytes = %x, %v", data, err)
	}
	read := dispatchForTest(t, dispatcher, "file.read", map[string]any{"path": path, "offset": 0, "limit": 2})
	requireOK(t, read)
	if read.Result["encoding"] != "base64" {
		t.Fatalf("binary read = %#v", read.Result)
	}
	decoded, err := base64.StdEncoding.DecodeString(read.Result["data"].(string))
	if err != nil || !bytes.Equal(decoded, binary[:2]) {
		t.Fatalf("binary range = %x, %v", decoded, err)
	}
}

func TestFileAppendPatchAndRemoval(t *testing.T) {
	dispatcher := NewDispatcher()
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0600); err != nil {
		t.Fatal(err)
	}
	appendResult := dispatchForTest(t, dispatcher, "file.append", map[string]any{"path": path, "content": "beta\n"})
	requireOK(t, appendResult)
	if appendResult.Result["bytes_appended"] != 5 || appendResult.Result["total_bytes"] != int64(11) {
		t.Fatalf("append result = %#v", appendResult.Result)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "alpha\nbeta\n" {
		t.Fatalf("after append = %q, %v", data, err)
	}

	patch := "--- note.txt\n+++ note.txt\n@@ -1,2 +1,2 @@\n alpha\n-beta\n+gamma\n"
	patched := dispatchForTest(t, dispatcher, "file.patch", map[string]any{"cwd": dir, "patch": patch, "strip": 0})
	requireOK(t, patched)
	if data, err := os.ReadFile(path); err != nil || string(data) != "alpha\ngamma\n" {
		t.Fatalf("after patch = %q, %v; stdout=%q stderr=%q", data, err, patched.Stdout, patched.Stderr)
	}

	childDir := filepath.Join(dir, "child")
	if err := os.Mkdir(childDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "x"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	nonrecursive := dispatchForTest(t, dispatcher, "file.remove", map[string]any{"path": childDir})
	if nonrecursive.OK {
		t.Fatal("nonrecursive removal unexpectedly removed a nonempty directory")
	}
	if _, err := os.Stat(filepath.Join(childDir, "x")); err != nil {
		t.Fatalf("child disappeared after failed nonrecursive removal: %v", err)
	}
	recursive := dispatchForTest(t, dispatcher, "file.remove", map[string]any{"path": childDir, "recursive": true})
	requireOK(t, recursive)
	if _, err := os.Stat(childDir); !os.IsNotExist(err) {
		t.Fatalf("recursive removal left directory: %v", err)
	}
	removed := dispatchForTest(t, dispatcher, "file.remove", map[string]any{"path": path})
	requireOK(t, removed)
	missing := dispatchForTest(t, dispatcher, "file.remove", map[string]any{"path": path, "missing_ok": true})
	requireOK(t, missing)
	if missing.Result["removed"] != false {
		t.Fatalf("missing_ok result = %#v", missing.Result)
	}
}
