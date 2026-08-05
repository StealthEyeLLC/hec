package hec

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPublishesOnlyCallHEC(t *testing.T) {
	ctx := context.Background()
	server := NewMCPServer(NewDispatcher())
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
	if len(branches) != 10 {
		t.Fatalf("input schema oneOf branch count = %d, want 10", len(branches))
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
		if !ok {
			t.Fatalf("operation const = %#v", operationSchema["const"])
		}
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	expectedOperations := []string{
		"health", "job.forget", "job.list", "job.output", "job.signal",
		"job.start", "job.status", "job.wait", "run", "version",
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

	validHandle := "job:AAAAAAAAAAAAAAAAAAAAAA"
	validationCases := []struct {
		name  string
		input map[string]any
		valid bool
	}{
		{name: "health empty args", input: map[string]any{"operation": "health", "args": map[string]any{}}, valid: true},
		{name: "version empty args", input: map[string]any{"operation": "version", "args": map[string]any{}}, valid: true},
		{name: "run argv", input: map[string]any{"operation": "run", "args": map[string]any{"argv": []any{"/usr/bin/id"}}}, valid: true},
		{name: "run command", input: map[string]any{"operation": "run", "args": map[string]any{"command": "id"}}, valid: true},
		{name: "job start argv", input: map[string]any{"operation": "job.start", "args": map[string]any{"argv": []any{"/bin/true"}}}, valid: true},
		{name: "job start command and key", input: map[string]any{"operation": "job.start", "args": map[string]any{"command": "true"}, "idempotency_key": "slice-2"}, valid: true},
		{name: "job status", input: map[string]any{"operation": "job.status", "args": map[string]any{"handle": validHandle}}, valid: true},
		{name: "job output", input: map[string]any{"operation": "job.output", "args": map[string]any{"handle": validHandle, "stream": "stdout"}}, valid: true},
		{name: "job wait", input: map[string]any{"operation": "job.wait", "args": map[string]any{"handle": validHandle}}, valid: true},
		{name: "job signal", input: map[string]any{"operation": "job.signal", "args": map[string]any{"handle": validHandle, "signal": "SIGTERM"}}, valid: true},
		{name: "job list", input: map[string]any{"operation": "job.list", "args": map[string]any{}}, valid: true},
		{name: "job forget", input: map[string]any{"operation": "job.forget", "args": map[string]any{"handle": validHandle}}, valid: true},

		{name: "unknown operation", input: map[string]any{"operation": "unknown", "args": map[string]any{}}, valid: false},
		{name: "health nonempty args", input: map[string]any{"operation": "health", "args": map[string]any{"unexpected": true}}, valid: false},
		{name: "version nonempty args", input: map[string]any{"operation": "version", "args": map[string]any{"unexpected": true}}, valid: false},
		{name: "run argv and command", input: map[string]any{"operation": "run", "args": map[string]any{"argv": []any{"/usr/bin/id"}, "command": "id"}}, valid: false},
		{name: "run neither", input: map[string]any{"operation": "run", "args": map[string]any{}}, valid: false},
		{name: "job start both command and argv", input: map[string]any{"operation": "job.start", "args": map[string]any{"command": "true", "argv": []any{"/bin/true"}}}, valid: false},
		{name: "job start neither command nor argv", input: map[string]any{"operation": "job.start", "args": map[string]any{}}, valid: false},
		{name: "job start both stdin forms", input: map[string]any{"operation": "job.start", "args": map[string]any{"command": "cat", "stdin": "a", "stdin_base64": "YQ=="}}, valid: false},
		{name: "unknown job start argument", input: map[string]any{"operation": "job.start", "args": map[string]any{"command": "true", "unexpected": true}}, valid: false},
		{name: "job status missing handle", input: map[string]any{"operation": "job.status", "args": map[string]any{}}, valid: false},
		{name: "job output invalid stream", input: map[string]any{"operation": "job.output", "args": map[string]any{"handle": validHandle, "stream": "both"}}, valid: false},
		{name: "job output negative offset", input: map[string]any{"operation": "job.output", "args": map[string]any{"handle": validHandle, "stream": "stdout", "offset": -1}}, valid: false},
		{name: "job output zero limit", input: map[string]any{"operation": "job.output", "args": map[string]any{"handle": validHandle, "stream": "stdout", "limit": 0}}, valid: false},
		{name: "job output negative limit", input: map[string]any{"operation": "job.output", "args": map[string]any{"handle": validHandle, "stream": "stdout", "limit": -1}}, valid: false},
		{name: "job wait missing handle", input: map[string]any{"operation": "job.wait", "args": map[string]any{}}, valid: false},
		{name: "job wait negative timeout", input: map[string]any{"operation": "job.wait", "args": map[string]any{"handle": validHandle, "timeout_ms": -1}}, valid: false},
		{name: "job signal missing signal", input: map[string]any{"operation": "job.signal", "args": map[string]any{"handle": validHandle}}, valid: false},
		{name: "job signal invalid signal", input: map[string]any{"operation": "job.signal", "args": map[string]any{"handle": validHandle, "signal": "SIGNOPE"}}, valid: false},
		{name: "job list nonempty args", input: map[string]any{"operation": "job.list", "args": map[string]any{"unexpected": true}}, valid: false},
		{name: "job forget missing handle", input: map[string]any{"operation": "job.forget", "args": map[string]any{}}, valid: false},
		{name: "unknown top-level field", input: map[string]any{"operation": "health", "args": map[string]any{}, "unexpected": true}, valid: false},
	}
	for _, tc := range validationCases {
		t.Run(tc.name, func(t *testing.T) {
			err := resolvedSchema.Validate(tc.input)
			if tc.valid && err != nil {
				t.Fatalf("schema rejected valid input: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("schema accepted invalid input")
			}
		})
	}

	called, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "call_hec",
		Arguments: map[string]any{
			"operation": "health",
			"args":      map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if called.IsError || len(called.Content) != 1 {
		t.Fatalf("tool result = %#v", called)
	}
	text, ok := called.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "HEC is alive." {
		t.Fatalf("text content = %#v", called.Content[0])
	}
	structured, ok := called.StructuredContent.(map[string]any)
	if !ok || structured["protocol"] != ProtocolVersion || structured["operation"] != "health" || structured["ok"] != true {
		t.Fatalf("structured content = %#v", called.StructuredContent)
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
