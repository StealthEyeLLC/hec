package hec

import (
	"context"
	"encoding/json"
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
	if tool.Name != "call_hec" {
		t.Fatalf("tool name = %q, want call_hec", tool.Name)
	}
	if tool.Title != "HEC" {
		t.Fatalf("tool title = %q, want HEC", tool.Title)
	}
	if tool.Description != "Operate the HEC workstation." {
		t.Fatalf("tool description = %q", tool.Description)
	}
	if tool.Annotations != nil {
		t.Fatalf("tool annotations = %#v, want nil", tool.Annotations)
	}

	schemaObject, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema type = %T, want map[string]any", tool.InputSchema)
	}
	branches, ok := schemaObject["oneOf"].([]any)
	if !ok {
		t.Fatalf("input schema oneOf = %#v", schemaObject["oneOf"])
	}
	if len(branches) != 3 {
		t.Fatalf("input schema oneOf branch count = %d, want 3", len(branches))
	}

	operations := make(map[string]bool, len(branches))
	for _, rawBranch := range branches {
		branch, ok := rawBranch.(map[string]any)
		if !ok {
			t.Fatalf("oneOf branch type = %T", rawBranch)
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
		operations[operation] = true
	}
	for _, operation := range []string{"health", "version", "run"} {
		if !operations[operation] {
			t.Errorf("operation %q is not explicitly enumerated", operation)
		}
	}
	if len(operations) != 3 {
		t.Fatalf("enumerated operations = %#v, want exactly health, version, run", operations)
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

	validationCases := []struct {
		name  string
		input map[string]any
		valid bool
	}{
		{
			name:  "health empty args",
			input: map[string]any{"operation": "health", "args": map[string]any{}},
			valid: true,
		},
		{
			name:  "version empty args",
			input: map[string]any{"operation": "version", "args": map[string]any{}},
			valid: true,
		},
		{
			name:  "unknown operation",
			input: map[string]any{"operation": "unknown", "args": map[string]any{}},
			valid: false,
		},
		{
			name:  "health nonempty args",
			input: map[string]any{"operation": "health", "args": map[string]any{"unexpected": true}},
			valid: false,
		},
		{
			name:  "version nonempty args",
			input: map[string]any{"operation": "version", "args": map[string]any{"unexpected": true}},
			valid: false,
		},
		{
			name:  "run argv",
			input: map[string]any{"operation": "run", "args": map[string]any{"argv": []any{"/usr/bin/id"}}},
			valid: true,
		},
		{
			name:  "run command",
			input: map[string]any{"operation": "run", "args": map[string]any{"command": "id"}},
			valid: true,
		},
		{
			name: "run argv and command",
			input: map[string]any{
				"operation": "run",
				"args":      map[string]any{"argv": []any{"/usr/bin/id"}, "command": "id"},
			},
			valid: false,
		},
		{
			name:  "run neither argv nor command",
			input: map[string]any{"operation": "run", "args": map[string]any{}},
			valid: false,
		},
		{
			name: "run stdin and stdin_base64",
			input: map[string]any{
				"operation": "run",
				"args": map[string]any{
					"argv":         []any{"/usr/bin/cat"},
					"stdin":        "text",
					"stdin_base64": "dGV4dA==",
				},
			},
			valid: false,
		},
		{
			name: "unknown run argument",
			input: map[string]any{
				"operation": "run",
				"args":      map[string]any{"argv": []any{"/usr/bin/id"}, "unexpected": true},
			},
			valid: false,
		},
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
	if called.IsError {
		t.Fatalf("tool marked error: %#v", called)
	}
	if len(called.Content) != 1 {
		t.Fatalf("content count = %d", len(called.Content))
	}
	text, ok := called.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "HEC is alive." {
		t.Fatalf("text content = %#v", called.Content[0])
	}
	structured, ok := called.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T", called.StructuredContent)
	}
	if structured["protocol"] != ProtocolVersion || structured["operation"] != "health" || structured["ok"] != true {
		t.Fatalf("structured content = %#v", structured)
	}
}
