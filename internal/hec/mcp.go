package hec

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const callInputSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {"type": "string", "minLength": 1},
    "args": {"type": "object", "additionalProperties": true},
    "idempotency_key": {"type": "string", "minLength": 1, "maxLength": 512}
  },
  "additionalProperties": false
}`

const callOutputSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": [
    "ok", "protocol", "operation", "status", "handle", "exit_code", "signal",
    "stdout", "stderr", "stdout_encoding", "stderr_encoding", "truncated",
    "result", "resources", "error"
  ],
  "properties": {
    "ok": {"type": "boolean"},
    "protocol": {"type": "string"},
    "operation": {"type": "string"},
    "status": {"type": "string"},
    "handle": {"type": ["string", "null"]},
    "exit_code": {"type": ["integer", "null"]},
    "signal": {"type": ["string", "null"]},
    "stdout": {"type": "string"},
    "stderr": {"type": "string"},
    "stdout_encoding": {"enum": ["utf8", "base64"]},
    "stderr_encoding": {"enum": ["utf8", "base64"]},
    "truncated": {"type": "boolean"},
    "result": {"type": "object"},
    "resources": {"type": "array"},
    "error": {"type": ["object", "null"]}
  },
  "additionalProperties": false
}`

func NewMCPServer(dispatcher *Dispatcher) *mcp.Server {
	if dispatcher == nil {
		dispatcher = NewDispatcher()
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "hec",
		Version: Version,
	}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:         "call_hec",
		Title:        "HEC",
		Description:  "Operate the HEC workstation.",
		InputSchema:  json.RawMessage(callInputSchemaJSON),
		OutputSchema: json.RawMessage(callOutputSchemaJSON),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallRequest) (*mcp.CallToolResult, Result, error) {
		result := dispatcher.Dispatch(ctx, input)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: result.Summary()},
			},
			IsError: !result.OK,
		}, result, nil
	})
	return server
}
