package hec

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConcurrentChatGPTWorkflowsReuseJSONRPCIDs10000Lifecycles(t *testing.T) {
	const (
		workflowCount         = 4
		lifecyclesPerWorkflow = 2500
		turnoverInterval      = 100
	)
	var active atomic.Int32
	var maximum atomic.Int32
	var dispatches atomic.Int64
	handler := newPublicCallHandler(dispatchFunc(func(ctx context.Context, request CallRequest) Result {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		sequence := dispatches.Add(1)
		marker := markerFromRunRequest(request)
		if marker == "" {
			result := failedResult(request.Operation, "missing_marker", "simulated request marker is missing")
			return result
		}
		if sequence%17 == 0 || len(marker) >= 7 && marker[:7] == "cancel-" {
			timer := time.NewTimer(250 * time.Microsecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				result := failedResult(request.Operation, "canceled", "simulated request was canceled")
				result.Result["marker"] = marker
				return result
			case <-timer.C:
			}
		}
		if sequence%29 == 0 {
			result := failedResult(request.Operation, "simulated_error", "simulated workflow error")
			result.Result["marker"] = marker
			return result
		}
		result := newResult(request.Operation)
		result.OK = true
		result.Result["marker"] = marker
		return result
	}))
	server := newMCPServer(handler)
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	transport := newRestartableMCPTransport(rootCtx, server)

	errCh := make(chan error, workflowCount)
	var wait sync.WaitGroup
	wait.Add(workflowCount)
	for workflow := 0; workflow < workflowCount; workflow++ {
		workflow := workflow
		go func() {
			defer wait.Done()
			connection, err := connectRawWorkflow(rootCtx, transport, workflow)
			if err != nil {
				errCh <- err
				return
			}
			defer connection.Close()

			for lifecycle := 0; lifecycle < lifecyclesPerWorkflow; lifecycle++ {
				marker := fmt.Sprintf("workflow-%d-lifecycle-%d", workflow, lifecycle)
				idValue := int64(lifecycle % 2)
				response, err := rawToolCall(rootCtx, connection, idValue, marker)
				if err != nil {
					errCh <- fmt.Errorf("%s: %w", marker, err)
					return
				}
				if got := response.ID.Raw(); got != idValue {
					errCh <- fmt.Errorf("%s: response ID = %#v, want %d", marker, got, idValue)
					return
				}
				gotMarker, err := markerFromRawToolResponse(response)
				if err != nil {
					errCh <- fmt.Errorf("%s: %w", marker, err)
					return
				}
				if gotMarker != marker {
					errCh <- fmt.Errorf("cross-response delivery: got %q, want %q", gotMarker, marker)
					return
				}

				if lifecycle%25 == 0 {
					if err := rawNotification(connection, "notifications/cancelled", map[string]any{
						"requestId": int64(999999),
						"reason":    "simulated notification",
					}); err != nil {
						errCh <- fmt.Errorf("%s notification: %w", marker, err)
						return
					}
				}

				if (lifecycle+1)%turnoverInterval == 0 {
					cancelMarker := fmt.Sprintf("cancel-workflow-%d-turnover-%d", workflow, lifecycle+1)
					cancelID := int64((lifecycle + 1) % 2)
					if err := rawToolWrite(connection, cancelID, cancelMarker); err != nil {
						errCh <- fmt.Errorf("%s write: %w", cancelMarker, err)
						return
					}
					if err := rawNotification(connection, "notifications/cancelled", map[string]any{
						"requestId": cancelID,
						"reason":    "controlled connection turnover",
					}); err != nil {
						errCh <- fmt.Errorf("%s cancel: %w", cancelMarker, err)
						return
					}
					if err := connection.Close(); err != nil {
						errCh <- fmt.Errorf("%s close: %w", cancelMarker, err)
						return
					}
					connection, err = connectRawWorkflow(rootCtx, transport, workflow)
					if err != nil {
						errCh <- fmt.Errorf("%s reconnect: %w", cancelMarker, err)
						return
					}
				}
			}
		}()
	}
	wait.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum active public Dispatch = %d, want 1", got)
	}
	if got := dispatches.Load(); got < workflowCount*lifecyclesPerWorkflow {
		t.Fatalf("complete dispatches = %d, want at least %d", got, workflowCount*lifecyclesPerWorkflow)
	}
	minimumConnections := uint64(workflowCount + workflowCount*(lifecyclesPerWorkflow/turnoverInterval))
	if transport.nextID < minimumConnections {
		t.Fatalf("fresh transport connections = %d, want at least %d", transport.nextID, minimumConnections)
	}
	cancelRoot()
	_ = transport.Close()
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCleanup()
	if err := transport.Wait(cleanupCtx); err != nil {
		t.Fatal(err)
	}
	if got := transport.liveWorkers(); got != 0 {
		t.Fatalf("live transport workers after regression = %d", got)
	}
}

func markerFromRunRequest(request CallRequest) string {
	argv, ok := request.Args["argv"].([]any)
	if !ok || len(argv) < 2 {
		if strings, ok := request.Args["argv"].([]string); ok && len(strings) >= 2 {
			return strings[1]
		}
		return ""
	}
	marker, _ := argv[1].(string)
	return marker
}

func connectRawWorkflow(ctx context.Context, transport mcp.Transport, workflow int) (mcp.Connection, error) {
	connection, err := transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	initializeID, err := jsonrpc.MakeID(float64(0))
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	initializeParams, _ := json.Marshal(map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    fmt.Sprintf("workflow-%d", workflow),
			"version": "1",
		},
	})
	if err := connection.Write(ctx, &jsonrpc.Request{ID: initializeID, Method: "initialize", Params: initializeParams}); err != nil {
		_ = connection.Close()
		return nil, err
	}
	message, err := connection.Read(ctx)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	response, ok := message.(*jsonrpc.Response)
	if !ok || response.Error != nil || response.ID.Raw() != int64(0) {
		_ = connection.Close()
		return nil, fmt.Errorf("initialize response = %#v", message)
	}
	if err := rawNotification(connection, "notifications/initialized", map[string]any{}); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return connection, nil
}

func rawToolCall(ctx context.Context, connection mcp.Connection, id int64, marker string) (*jsonrpc.Response, error) {
	if err := rawToolWrite(connection, id, marker); err != nil {
		return nil, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		message, err := connection.Read(readCtx)
		if err != nil {
			return nil, err
		}
		switch typed := message.(type) {
		case *jsonrpc.Request:
			if typed.ID.IsValid() {
				return nil, fmt.Errorf("unexpected server request %q with ID %#v", typed.Method, typed.ID.Raw())
			}
			continue
		case *jsonrpc.Response:
			if typed.Error != nil {
				return nil, fmt.Errorf("JSON-RPC error: %v", typed.Error)
			}
			return typed, nil
		default:
			return nil, fmt.Errorf("response type = %T", message)
		}
	}
}

func rawToolWrite(connection mcp.Connection, id int64, marker string) error {
	requestID, err := jsonrpc.MakeID(float64(id))
	if err != nil {
		return err
	}
	params, err := json.Marshal(map[string]any{
		"name": "call_hec",
		"arguments": map[string]any{
			"operation": "run",
			"args": map[string]any{
				"argv": []any{"/bin/echo", marker},
			},
		},
	})
	if err != nil {
		return err
	}
	return connection.Write(context.Background(), &jsonrpc.Request{ID: requestID, Method: "tools/call", Params: params})
}

func rawNotification(connection mcp.Connection, method string, paramsValue any) error {
	params, err := json.Marshal(paramsValue)
	if err != nil {
		return err
	}
	return connection.Write(context.Background(), &jsonrpc.Request{Method: method, Params: params})
}

func markerFromRawToolResponse(response *jsonrpc.Response) (string, error) {
	var payload struct {
		StructuredContent struct {
			Result map[string]any `json:"result"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(response.Result, &payload); err != nil {
		return "", err
	}
	marker, ok := payload.StructuredContent.Result["marker"].(string)
	if !ok || marker == "" {
		return "", fmt.Errorf("missing marker in response %s", response.Result)
	}
	return marker, nil
}
