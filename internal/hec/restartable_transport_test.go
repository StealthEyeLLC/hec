package hec

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRestartableTransportCreatesFreshPairsAndCloseIsIsolated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := newRestartableMCPTransport(ctx, NewMCPServer(NewDispatcher()))
	defer transport.Close()

	first, err := transport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := transport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("Connect reused one MCP connection")
	}
	if transport.nextID != 2 {
		t.Fatalf("connection generation count = %d, want 2", transport.nextID)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	waitForLiveMCPSessions(t, transport, 1)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	waitForLiveMCPSessions(t, transport, 0)
}

func TestRestartableTransportClosedConnectionDoesNotPoisonLaterConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := newRestartableMCPTransport(ctx, NewMCPServer(NewDispatcher()))
	defer transport.Close()

	firstClient := mcp.NewClient(&mcp.Implementation{Name: "first", Version: "1"}, nil)
	firstSession, err := firstClient.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstResult := callHECTestOperation(t, ctx, firstSession, "health")
	if firstResult["operation"] != "health" {
		t.Fatalf("first response = %#v", firstResult)
	}
	if err := firstSession.Close(); err != nil {
		t.Fatal(err)
	}
	waitForLiveMCPSessions(t, transport, 0)

	secondClient := mcp.NewClient(&mcp.Implementation{Name: "second", Version: "1"}, nil)
	secondSession, err := secondClient.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer secondSession.Close()
	secondResult := callHECTestOperation(t, ctx, secondSession, "version")
	if secondResult["operation"] != "version" {
		t.Fatalf("old response crossed into later connection: %#v", secondResult)
	}
}

func TestRestartableTransportConnectionCloseCancelsMatchingSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := newRestartableMCPTransport(ctx, NewMCPServer(NewDispatcher()))
	defer transport.Close()
	connection, err := transport.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	var session *restartableMCPSession
	for _, candidate := range transport.sessions {
		session = candidate
	}
	transport.mu.Unlock()
	if session == nil {
		t.Fatal("matching server session was not tracked")
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("connection close did not cancel matching server session")
	}
	waitForLiveMCPSessions(t, transport, 0)
}

func TestRestartableTransportRootStopClosesAllSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	transport := newRestartableMCPTransport(ctx, NewMCPServer(NewDispatcher()))
	for range 3 {
		if _, err := transport.Connect(ctx); err != nil {
			t.Fatal(err)
		}
	}
	waitForLiveMCPSessions(t, transport, 3)
	cancel()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()
	if err := transport.Wait(cleanupCtx); err != nil {
		t.Fatal(err)
	}
	if got := transport.liveWorkers(); got != 0 {
		t.Fatalf("live sessions after root stop = %d", got)
	}
}

func TestRestartableTransportUnexpectedSessionClosureSignalsGenerationFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport := newRestartableMCPTransport(ctx, NewMCPServer(NewDispatcher()))
	defer transport.Close()
	if _, err := transport.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	var session *restartableMCPSession
	for _, candidate := range transport.sessions {
		session = candidate
	}
	transport.mu.Unlock()
	if session == nil {
		t.Fatal("tracked session missing")
	}
	if err := session.serverSession.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-transport.Failures():
		if err == nil {
			t.Fatal("nil generation failure")
		}
	case <-time.After(time.Second):
		t.Fatal("forced server-session closure was not reported")
	}
}

func callHECTestOperation(t *testing.T, ctx context.Context, session *mcp.ClientSession, operation string) map[string]any {
	t.Helper()
	called, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "call_hec",
		Arguments: map[string]any{
			"operation": operation,
			"args":      map[string]any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	structured, ok := called.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured response type = %T", called.StructuredContent)
	}
	return structured
}

func waitForLiveMCPSessions(t *testing.T, transport *restartableMCPTransport, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if got := transport.liveWorkers(); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("live MCP sessions = %d, want %d", transport.liveWorkers(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRestartableTransportConnectContextCancellationClosesMatchingSession(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	transport := newRestartableMCPTransport(rootCtx, NewMCPServer(NewDispatcher()))
	defer transport.Close()
	connectCtx, cancelConnect := context.WithCancel(context.Background())
	if _, err := transport.Connect(connectCtx); err != nil {
		t.Fatal(err)
	}
	waitForLiveMCPSessions(t, transport, 1)
	cancelConnect()
	waitForLiveMCPSessions(t, transport, 0)
}

func TestRestartableTransportCarriesConnectDeadlineIntoServerSession(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	transport := newRestartableMCPTransport(rootCtx, NewMCPServer(NewDispatcher()))
	defer transport.Close()
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelConnect()
	connection, err := transport.Connect(connectCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	transport.mu.Lock()
	var session *restartableMCPSession
	for _, candidate := range transport.sessions {
		session = candidate
	}
	transport.mu.Unlock()
	if session == nil {
		t.Fatal("matching server session missing")
	}
	callerDeadline, callerOK := connectCtx.Deadline()
	sessionDeadline, sessionOK := session.ctx.Deadline()
	if !callerOK || !sessionOK {
		t.Fatalf("caller deadline=%v session deadline=%v", callerOK, sessionOK)
	}
	difference := sessionDeadline.Sub(callerDeadline)
	if difference < -time.Millisecond || difference > time.Millisecond {
		t.Fatalf("session deadline differs from tunnel caller by %s", difference)
	}
}
