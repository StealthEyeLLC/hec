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
	mcp.AddTool(server, &mcp.Tool{
		Name:         "call_hec",
		Title:        "HEC",
		Description:  "Operate the HEC workstation.",
		InputSchema:  json.RawMessage(hecschemas.CallHECInput),
		OutputSchema: json.RawMessage(hecschemas.CallHECOutput),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallRequest) (*mcp.CallToolResult, Result, error) {
		result := dispatcher.Dispatch(ctx, input)
		content := []mcp.Content{&mcp.TextContent{Text: result.Summary()}}
		for _, rawDescriptor := range result.Resources {
			descriptor, ok := rawDescriptor.(ResourceDescriptor)
			if !ok {
				continue
			}
			size := descriptor.Size
			content = append(content, &mcp.ResourceLink{
				URI:         descriptor.URI,
				Name:        descriptor.Name,
				Title:       descriptor.Name,
				Description: "HEC returned artifact.",
				MIMEType:    descriptor.MediaType,
				Size:        &size,
			})
		}
		return &mcp.CallToolResult{
			Content: content,
			IsError: !result.OK,
		}, result, nil
	})
	return server
}
