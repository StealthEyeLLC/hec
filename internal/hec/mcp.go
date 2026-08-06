package hec

import (
	"context"
	"encoding/json"

	hecschemas "github.com/StealthEyeLLC/hec/schemas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewMCPServer(dispatcher *Dispatcher) *mcp.Server {
	if dispatcher == nil {
		dispatcher = NewDispatcher()
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "hec",
		Version: Version,
	}, nil)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "hec://artifact/{id}",
		Name:        "HEC artifact",
		Description: "Read an immutable artifact returned by HEC.",
	}, dispatcher.readArtifactResource)

	falseValue := false

	mcp.AddTool(server, &mcp.Tool{
		Name:         "call_hec",
		Title:        "HEC",
		Description:  "Operate the HEC workstation.",
		InputSchema:  json.RawMessage(hecschemas.CallHECInput),
		OutputSchema: json.RawMessage(hecschemas.CallHECOutput),
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &falseValue,
			IdempotentHint:  true,
		},
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input CallRequest,
	) (*mcp.CallToolResult, Result, error) {
		result := dispatcher.Dispatch(ctx, input)

		// Return text and structured output only. Emitting an MCP ResourceLink here
		// causes ChatGPT to materialize the artifact into the conversation and show
		// a separate approval prompt.
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: result.Summary()},
			},
			IsError: !result.OK,
		}, result, nil
	})

	return server
}
