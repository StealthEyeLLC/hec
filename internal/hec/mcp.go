package hec

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const callInputSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "HEC call",
  "description": "Select one supported HEC operation and provide only the arguments defined for that operation.",
  "type": "object",
  "oneOf": [
    {
      "title": "Health",
      "description": "Read HEC service liveness. This operation does not change workstation state.",
      "type": "object",
      "required": [
        "operation",
        "args"
      ],
      "properties": {
        "operation": {
          "const": "health",
          "description": "Read HEC service liveness."
        },
        "args": {
          "type": "object",
          "description": "No arguments.",
          "properties": {},
          "additionalProperties": false
        },
        "idempotency_key": {
          "type": "string",
          "minLength": 1,
          "maxLength": 512,
          "description": "Optional request key. It has no effect for health and should normally be omitted."
        }
      },
      "additionalProperties": false
    },
    {
      "title": "Version",
      "description": "Read the installed HEC and protocol versions. This operation does not change workstation state.",
      "type": "object",
      "required": [
        "operation",
        "args"
      ],
      "properties": {
        "operation": {
          "const": "version",
          "description": "Read installed HEC version information."
        },
        "args": {
          "type": "object",
          "description": "No arguments.",
          "properties": {},
          "additionalProperties": false
        },
        "idempotency_key": {
          "type": "string",
          "minLength": 1,
          "maxLength": 512,
          "description": "Optional request key. It has no effect for version and should normally be omitted."
        }
      },
      "additionalProperties": false
    },
    {
      "title": "Run",
      "description": "Run one noninteractive process on HEC using either a shell command or a direct argument vector.",
      "type": "object",
      "required": [
        "operation",
        "args"
      ],
      "properties": {
        "operation": {
          "const": "run",
          "description": "Run one noninteractive process."
        },
        "args": {
          "description": "Exactly one of command or argv is required. stdin and stdin_base64 are mutually exclusive.",
          "oneOf": [
            {
              "title": "Shell command",
              "type": "object",
              "required": [
                "command"
              ],
              "properties": {
                "command": {
                  "type": "string",
                  "description": "Shell command executed by /bin/bash -lc."
                },
                "cwd": {
                  "type": "string",
                  "minLength": 1,
                  "description": "Working directory. Omit to use /root."
                },
                "env": {
                  "type": "object",
                  "description": "Environment variables to add or replace.",
                  "additionalProperties": {
                    "type": "string"
                  }
                },
                "unset_env": {
                  "type": "array",
                  "description": "Environment variable names to remove.",
                  "items": {
                    "type": "string",
                    "minLength": 1
                  },
                  "uniqueItems": true
                },
                "stdin": {
                  "type": "string",
                  "description": "UTF-8 standard input."
                },
                "stdin_base64": {
                  "type": "string",
                  "minLength": 1,
                  "description": "Base64-encoded binary standard input."
                },
                "timeout_ms": {
                  "type": "integer",
                  "minimum": 0,
                  "description": "Timeout in milliseconds. Zero or omission means no HEC timeout."
                },
                "max_output_bytes": {
                  "type": "integer",
                  "minimum": 0,
                  "description": "Maximum combined inline stdout and stderr bytes. Zero or omission uses the 1 MiB default."
                }
              },
              "not": {
                "required": [
                  "stdin",
                  "stdin_base64"
                ]
              },
              "additionalProperties": false
            },
            {
              "title": "Direct argument vector",
              "type": "object",
              "required": [
                "argv"
              ],
              "properties": {
                "argv": {
                  "type": "array",
                  "minItems": 1,
                  "description": "Executable path or name followed by its exact arguments.",
                  "prefixItems": [
                    {
                      "type": "string",
                      "minLength": 1,
                      "description": "Executable path or command name."
                    }
                  ],
                  "items": {
                    "type": "string"
                  }
                },
                "cwd": {
                  "type": "string",
                  "minLength": 1,
                  "description": "Working directory. Omit to use /root."
                },
                "env": {
                  "type": "object",
                  "description": "Environment variables to add or replace.",
                  "additionalProperties": {
                    "type": "string"
                  }
                },
                "unset_env": {
                  "type": "array",
                  "description": "Environment variable names to remove.",
                  "items": {
                    "type": "string",
                    "minLength": 1
                  },
                  "uniqueItems": true
                },
                "stdin": {
                  "type": "string",
                  "description": "UTF-8 standard input."
                },
                "stdin_base64": {
                  "type": "string",
                  "minLength": 1,
                  "description": "Base64-encoded binary standard input."
                },
                "timeout_ms": {
                  "type": "integer",
                  "minimum": 0,
                  "description": "Timeout in milliseconds. Zero or omission means no HEC timeout."
                },
                "max_output_bytes": {
                  "type": "integer",
                  "minimum": 0,
                  "description": "Maximum combined inline stdout and stderr bytes. Zero or omission uses the 1 MiB default."
                }
              },
              "not": {
                "required": [
                  "stdin",
                  "stdin_base64"
                ]
              },
              "additionalProperties": false
            }
          ]
        },
        "idempotency_key": {
          "type": "string",
          "minLength": 1,
          "maxLength": 512,
          "description": "Optional request key. It has no effect for run and should normally be omitted."
        }
      },
      "additionalProperties": false
    }
  ]
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
