package hec

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSecureHandleGenerationAndParsing(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 128; i++ {
		id, err := secureID()
		if err != nil {
			t.Fatalf("secureID: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("id length = %d, want 32", len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
		if parsed, err := parseTypedHandle("upload:"+id, "upload"); err != nil || parsed != id {
			t.Fatalf("parse upload handle = %q, %v", parsed, err)
		}
		if parsed, err := parseTypedHandle("artifact:"+id, "artifact"); err != nil || parsed != id {
			t.Fatalf("parse artifact handle = %q, %v", parsed, err)
		}
	}
}

func TestHandleParsingRejectsMalformedAndTraversal(t *testing.T) {
	cases := []string{
		"",
		"upload:",
		"upload:../../etc/passwd",
		"upload:0123456789abcdef0123456789abcde/",
		"upload:0123456789ABCDEF0123456789ABCDEF",
		"artifact:0123456789abcdef0123456789abcdef",
		"upload:0123456789abcdef0123456789abcdeg",
	}
	for _, handle := range cases {
		if _, err := parseTypedHandle(handle, "upload"); err == nil {
			t.Fatalf("accepted malformed handle %q", handle)
		}
	}
}

func TestUTF8AndBase64Encoding(t *testing.T) {
	text, encoding := encodeData([]byte("hello π"))
	if encoding != "utf8" || text != "hello π" {
		t.Fatalf("utf8 = %q %q", encoding, text)
	}
	binary := []byte{0xff, 0xfe, 0x00, 0x41}
	encoded, encoding := encodeData(binary)
	if encoding != "base64" {
		t.Fatalf("binary encoding = %q", encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !bytes.Equal(decoded, binary) {
		t.Fatalf("base64 round trip = %x, %v", decoded, err)
	}
}

func TestBoundedRangeReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "range.bin")
	payload := []byte("0123456789")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	data, next, total, eof, err := readRange(file, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "234" || next != 5 || total != 10 || eof {
		t.Fatalf("range = %q next=%d total=%d eof=%v", data, next, total, eof)
	}
	data, next, total, eof, err = readRange(file, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 || next != 10 || total != 10 || !eof {
		t.Fatalf("end range = %q next=%d total=%d eof=%v", data, next, total, eof)
	}
	data, next, total, eof, err = readRange(file, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 || next != 100 || total != 10 || !eof {
		t.Fatalf("beyond range = %q next=%d total=%d eof=%v", data, next, total, eof)
	}
}
