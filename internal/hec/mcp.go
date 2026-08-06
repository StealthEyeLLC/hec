package hec

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime/debug"
	"strings"
	"time"

	hecschemas "github.com/StealthEyeLLC/hec/schemas"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type publicDispatcher interface {
	Dispatch(context.Context, CallRequest) Result
}

type callGate struct {
	token          chan struct{}
	acquireTimeout time.Duration
}

func newCallGate() *callGate {
	return &callGate{
		token:          make(chan struct{}, 1),
		acquireTimeout: CallGateAcquireTimeout,
	}
}

func (g *callGate) acquire(ctx context.Context) error {
	if g == nil {
		return nil
	}
	timeout := g.acquireTimeout
	if timeout <= 0 {
		timeout = CallGateAcquireTimeout
	}
	acquireCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case g.token <- struct{}{}:
		return nil
	case <-acquireCtx.Done():
		return acquireCtx.Err()
	}
}

func (g *callGate) release() {
	if g == nil {
		return
	}
	select {
	case <-g.token:
	default:
		panic("call gate released without acquisition")
	}
}

type publicCallHandler struct {
	dispatcher publicDispatcher
	gate       *callGate
}

func newPublicCallHandler(dispatcher publicDispatcher) *publicCallHandler {
	if dispatcher == nil {
		dispatcher = NewDispatcher()
	}
	return &publicCallHandler{dispatcher: dispatcher, gate: newCallGate()}
}

func (h *publicCallHandler) dispatch(ctx context.Context, input CallRequest) (result Result) {
	callCtx, cancel := directCallContext(ctx)
	defer cancel()

	if err := h.gate.acquire(callCtx); err != nil {
		if errors.Is(err, context.Canceled) {
			return failedResult(input.Operation, "canceled", "call was canceled while waiting for the dispatch gate")
		}
		return failedResult(input.Operation, "queue_timeout", "call could not acquire the dispatch gate within ten seconds")
	}
	defer h.gate.release()

	defer func() {
		if recover() != nil {
			log.Printf("call_hec handler panic; stack=%s", redactedStack())
			result = failedResult(input.Operation, "internal_error", "call_hec handler failed internally")
		}
	}()

	if err := callCtx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return failedResult(input.Operation, "canceled", "call was canceled")
		}
		return failedResult(input.Operation, "timed_out", "call deadline expired before dispatch")
	}
	result = h.dispatcher.Dispatch(callCtx, input)
	if err := callCtx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return failedResult(input.Operation, "canceled", "call was canceled before response delivery")
		}
		return failedResult(input.Operation, "timed_out", "call deadline expired before response delivery")
	}
	return result
}

func redactedStack() string {
	lines := strings.Split(string(debug.Stack()), "\n")
	functions := make([]string, 0, 16)
	for index := 1; index < len(lines); index += 2 {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		if position := strings.IndexByte(line, '('); position >= 0 {
			line = line[:position]
		}
		functions = append(functions, line)
		if len(functions) == 16 {
			break
		}
	}
	return strings.Join(functions, ";")
}

func NewMCPServer(dispatcher *Dispatcher) *mcp.Server {
	return newMCPServer(newPublicCallHandler(dispatcher))
}

func newMCPServer(handler *publicCallHandler) *mcp.Server {
	if handler == nil {
		handler = newPublicCallHandler(nil)
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "hec",
		Version: Version,
	}, nil)
	if dispatcher, ok := handler.dispatcher.(*Dispatcher); ok {
		server.AddResourceTemplate(&mcp.ResourceTemplate{
			URITemplate: "hec://artifact/{id}",
			Name:        "HEC artifact",
			Description: "Read an immutable artifact returned by HEC.",
		}, dispatcher.readArtifactResource)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:         "call_hec",
		Title:        "HEC",
		Description:  "Operate the HEC workstation.",
		InputSchema:  json.RawMessage(hecschemas.CallHECInput),
		OutputSchema: json.RawMessage(hecschemas.CallHECOutput),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CallRequest) (*mcp.CallToolResult, Result, error) {
		result := handler.dispatch(ctx, input)
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
