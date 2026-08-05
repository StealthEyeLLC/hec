package hec

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPublishesSlice3InterfaceAndArtifactResource(t *testing.T) {
	ctx := context.Background()
	dispatcher := NewDispatcher()
	dispatcher.uploadsDir = filepath.Join(t.TempDir(), "uploads")
	dispatcher.artifactsDir = filepath.Join(t.TempDir(), "artifacts")

	server := NewMCPServer(dispatcher)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "hec-test", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(listed.Tools))
	}
	tool := listed.Tools[0]
	if tool.Name != "call_hec" || tool.Title != "HEC" || tool.Description != "Operate the HEC workstation." {
		t.Fatalf("tool metadata = %#v", tool)
	}
	if tool.Annotations != nil {
		t.Fatalf("tool annotations = %#v, want nil", tool.Annotations)
	}

	schemaObject, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema type = %T, want map[string]any", tool.InputSchema)
	}
	if schemaObject["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("schema dialect = %#v", schemaObject["$schema"])
	}
	branches, ok := schemaObject["oneOf"].([]any)
	if !ok {
		t.Fatalf("input schema oneOf = %#v", schemaObject["oneOf"])
	}
	if len(branches) != 27 {
		t.Fatalf("input schema oneOf branch count = %d, want 27", len(branches))
	}

	operations := make([]string, 0, len(branches))
	for _, rawBranch := range branches {
		branch, ok := rawBranch.(map[string]any)
		if !ok {
			t.Fatalf("oneOf branch type = %T", rawBranch)
		}
		if branch["additionalProperties"] != false {
			t.Fatalf("branch allows unknown top-level fields: %#v", branch)
		}
		required, ok := branch["required"].([]any)
		if !ok || !containsString(required, "operation") || !containsString(required, "args") {
			t.Fatalf("branch required fields = %#v", branch["required"])
		}
		properties, ok := branch["properties"].(map[string]any)
		if !ok {
			t.Fatalf("branch properties = %#v", branch["properties"])
		}
		operationSchema, ok := properties["operation"].(map[string]any)
		if !ok {
			t.Fatalf("operation schema = %#v", properties["operation"])
		}
		operation, ok := operationSchema["const"].(string)
		if !ok || operation == "" {
			t.Fatalf("operation const = %#v", operationSchema["const"])
		}
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	expectedOperations := []string{
		"artifact.delete", "artifact.list", "artifact.materialize", "artifact.read", "artifact.return", "artifact.stat",
		"file.append", "file.list", "file.patch", "file.read", "file.remove", "file.stat", "file.write",
		"health", "job.forget", "job.list", "job.output", "job.signal", "job.start", "job.status", "job.wait",
		"run", "upload.abort", "upload.begin", "upload.chunk", "upload.finish", "version",
	}
	if !reflect.DeepEqual(operations, expectedOperations) {
		t.Fatalf("enumerated operations = %#v, want %#v", operations, expectedOperations)
	}

	schemaJSON, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	var parsedSchema jsonschema.Schema
	if err := json.Unmarshal(schemaJSON, &parsedSchema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	resolvedSchema, err := parsedSchema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve input schema: %v", err)
	}

	jobHandle := "job:AAAAAAAAAAAAAAAAAAAAAA"
	uploadHandle := "upload:0123456789abcdef0123456789abcdef"
	artifactHandle := "artifact:0123456789abcdef0123456789abcdef"
	validCases := map[string]map[string]any{
		"health":               {"operation": "health", "args": map[string]any{}},
		"version":              {"operation": "version", "args": map[string]any{}},
		"run":                  {"operation": "run", "args": map[string]any{"argv": []any{"/usr/bin/id"}}},
		"job.start":            {"operation": "job.start", "args": map[string]any{"argv": []any{"/bin/true"}}},
		"job.status":           {"operation": "job.status", "args": map[string]any{"handle": jobHandle}},
		"job.output":           {"operation": "job.output", "args": map[string]any{"handle": jobHandle, "stream": "stdout"}},
		"job.wait":             {"operation": "job.wait", "args": map[string]any{"handle": jobHandle}},
		"job.signal":           {"operation": "job.signal", "args": map[string]any{"handle": jobHandle, "signal": "SIGTERM"}},
		"job.list":             {"operation": "job.list", "args": map[string]any{}},
		"job.forget":           {"operation": "job.forget", "args": map[string]any{"handle": jobHandle}},
		"file.stat":            {"operation": "file.stat", "args": map[string]any{"path": "/tmp/a"}},
		"file.list":            {"operation": "file.list", "args": map[string]any{"path": "/tmp"}},
		"file.read":            {"operation": "file.read", "args": map[string]any{"path": "/tmp/a"}},
		"file.write.content":   {"operation": "file.write", "args": map[string]any{"path": "/tmp/a", "content": "x"}},
		"file.write.base64":    {"operation": "file.write", "args": map[string]any{"path": "/tmp/a", "content_base64": "AA=="}},
		"file.append":          {"operation": "file.append", "args": map[string]any{"path": "/tmp/a", "content": "x"}},
		"file.patch":           {"operation": "file.patch", "args": map[string]any{"cwd": "/tmp", "patch": "--- a/a\n+++ b/a\n"}},
		"file.remove":          {"operation": "file.remove", "args": map[string]any{"path": "/tmp/a"}},
		"upload.begin":         {"operation": "upload.begin", "args": map[string]any{"filename": "a.bin"}},
		"upload.chunk":         {"operation": "upload.chunk", "args": map[string]any{"handle": uploadHandle, "offset": 0, "data_base64": "AA=="}},
		"upload.finish.dest":   {"operation": "upload.finish", "args": map[string]any{"handle": uploadHandle, "destination": "/tmp/a"}},
		"upload.finish.art":    {"operation": "upload.finish", "args": map[string]any{"handle": uploadHandle, "artifact": true}},
		"upload.abort":         {"operation": "upload.abort", "args": map[string]any{"handle": uploadHandle}},
		"artifact.return":      {"operation": "artifact.return", "args": map[string]any{"path": "/tmp/a"}},
		"artifact.stat":        {"operation": "artifact.stat", "args": map[string]any{"handle": artifactHandle}},
		"artifact.read":        {"operation": "artifact.read", "args": map[string]any{"handle": artifactHandle}},
		"artifact.materialize": {"operation": "artifact.materialize", "args": map[string]any{"handle": artifactHandle, "destination": "/tmp/a"}},
		"artifact.list":        {"operation": "artifact.list", "args": map[string]any{}},
		"artifact.delete":      {"operation": "artifact.delete", "args": map[string]any{"handle": artifactHandle}},
	}
	for name, input := range validCases {
		t.Run("schema valid "+name, func(t *testing.T) {
			if err := resolvedSchema.Validate(input); err != nil {
				t.Fatalf("schema rejected valid input: %v", err)
			}
		})
	}

	invalidCases := map[string]map[string]any{
		"unknown operation":         {"operation": "unknown", "args": map[string]any{}},
		"file stat no path":         {"operation": "file.stat", "args": map[string]any{}},
		"file list negative offset": {"operation": "file.list", "args": map[string]any{"path": "/tmp", "offset": -1}},
		"file list zero limit":      {"operation": "file.list", "args": map[string]any{"path": "/tmp", "limit": 0}},
		"file read negative offset": {"operation": "file.read", "args": map[string]any{"path": "/tmp/a", "offset": -1}},
		"file read above max":       {"operation": "file.read", "args": map[string]any{"path": "/tmp/a", "limit": 1048577}},
		"file write neither":        {"operation": "file.write", "args": map[string]any{"path": "/tmp/a"}},
		"file write both":           {"operation": "file.write", "args": map[string]any{"path": "/tmp/a", "content": "x", "content_base64": "eA=="}},
		"file append both":          {"operation": "file.append", "args": map[string]any{"path": "/tmp/a", "content": "x", "content_base64": "eA=="}},
		"file patch no patch":       {"operation": "file.patch", "args": map[string]any{"cwd": "/tmp"}},
		"file remove no path":       {"operation": "file.remove", "args": map[string]any{}},
		"upload begin path name":    {"operation": "upload.begin", "args": map[string]any{"filename": "dir/a.bin"}},
		"upload chunk no data":      {"operation": "upload.chunk", "args": map[string]any{"handle": uploadHandle, "offset": 0}},
		"upload chunk negative":     {"operation": "upload.chunk", "args": map[string]any{"handle": uploadHandle, "offset": -1, "data_base64": "AA=="}},
		"upload finish neither":     {"operation": "upload.finish", "args": map[string]any{"handle": uploadHandle}},
		"upload finish both":        {"operation": "upload.finish", "args": map[string]any{"handle": uploadHandle, "destination": "/tmp/a", "artifact": true}},
		"upload finish relative":    {"operation": "upload.finish", "args": map[string]any{"handle": uploadHandle, "destination": "tmp/a"}},
		"upload abort no handle":    {"operation": "upload.abort", "args": map[string]any{}},
		"artifact return no path":   {"operation": "artifact.return", "args": map[string]any{}},
		"artifact stat upload":      {"operation": "artifact.stat", "args": map[string]any{"handle": uploadHandle}},
		"artifact read negative":    {"operation": "artifact.read", "args": map[string]any{"handle": artifactHandle, "offset": -1}},
		"artifact materialize rel":  {"operation": "artifact.materialize", "args": map[string]any{"handle": artifactHandle, "destination": "tmp/a"}},
		"artifact delete no handle": {"operation": "artifact.delete", "args": map[string]any{}},
	}
	for name, input := range invalidCases {
		t.Run("schema invalid "+name, func(t *testing.T) {
			if err := resolvedSchema.Validate(input); err == nil {
				t.Fatal("schema accepted invalid input")
			}
		})
	}
	for name, input := range validCases {
		operation, _ := input["operation"].(string)
		if operation == "health" || operation == "version" || operation == "run" || len(operation) >= 4 && operation[:4] == "job." {
			continue
		}
		copyInput := map[string]any{"operation": operation, "args": map[string]any{}}
		for key, value := range input["args"].(map[string]any) {
			copyInput["args"].(map[string]any)[key] = value
		}
		copyInput["args"].(map[string]any)["unexpected"] = true
		t.Run("schema rejects unknown "+name, func(t *testing.T) {
			if err := resolvedSchema.Validate(copyInput); err == nil {
				t.Fatal("schema accepted unknown Slice 3 argument")
			}
		})
	}

	source := filepath.Join(t.TempDir(), "artifact.bin")
	payload := []byte{0x00, 0xff, 0x41, 0x42}
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	called, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "call_hec",
		Arguments: map[string]any{
			"operation": "artifact.return",
			"args":      map[string]any{"path": source, "name": "artifact.bin", "media_type": "application/octet-stream"},
		},
	})
	if err != nil {
		t.Fatalf("call artifact.return: %v", err)
	}
	if called.IsError || len(called.Content) != 2 {
		t.Fatalf("tool result = %#v", called)
	}
	if text, ok := called.Content[0].(*mcp.TextContent); !ok || text.Text == "" {
		t.Fatalf("summary content = %#v", called.Content[0])
	}
	link, ok := called.Content[1].(*mcp.ResourceLink)
	if !ok {
		t.Fatalf("resource content type = %T", called.Content[1])
	}
	if link.Name != "artifact.bin" || link.MIMEType != "application/octet-stream" || link.URI == "" {
		t.Fatalf("resource link = %#v", link)
	}
	structured, ok := called.StructuredContent.(map[string]any)
	if !ok || structured["operation"] != "artifact.return" || structured["ok"] != true {
		t.Fatalf("structured content = %#v", called.StructuredContent)
	}
	read, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: link.URI})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("resource contents = %#v", read.Contents)
	}
	content := read.Contents[0]
	if content.MIMEType != "application/octet-stream" || content.Meta["name"] != "artifact.bin" || !bytes.Equal(content.Blob, payload) {
		t.Fatalf("resource = %#v", content)
	}
}

func containsString(values []any, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
