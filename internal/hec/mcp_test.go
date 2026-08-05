package hec

import (
	"context"
	"testing"

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
