package hec

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errRestartableTransportClosed = errors.New("restartable MCP transport is closed")

type restartableMCPTransport struct {
	ctx    context.Context
	cancel context.CancelFunc
	server *mcp.Server

	mu        sync.Mutex
	closed    bool
	nextID    uint64
	sessions  map[uint64]*restartableMCPSession
	workers   sync.WaitGroup
	failure   chan error
	closeOnce sync.Once
}

type restartableMCPSession struct {
	owner         *restartableMCPTransport
	id            uint64
	ctx           context.Context
	cancel        context.CancelFunc
	client        mcp.Connection
	serverSession *mcp.ServerSession
	closing       atomic.Bool
	closeOnce     sync.Once
	done          chan struct{}
}

type restartableMCPConnection struct {
	session *restartableMCPSession
	base    mcp.Connection
}

func newRestartableMCPTransport(parent context.Context, server *mcp.Server) *restartableMCPTransport {
	if parent == nil {
		parent = context.Background()
	}
	if server == nil {
		server = NewMCPServer(NewDispatcher())
	}
	ctx, cancel := context.WithCancel(parent)
	transport := &restartableMCPTransport{
		ctx:      ctx,
		cancel:   cancel,
		server:   server,
		sessions: make(map[uint64]*restartableMCPSession),
		failure:  make(chan error, 1),
	}
	transport.workers.Add(1)
	go func() {
		defer transport.workers.Done()
		<-ctx.Done()
		_ = transport.Close()
	}()
	return transport
}

func (t *restartableMCPTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-t.ctx.Done():
		return nil, errRestartableTransportClosed
	default:
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	var sessionCtx context.Context
	var cancel context.CancelFunc
	if deadline, ok := ctx.Deadline(); ok {
		sessionCtx, cancel = context.WithDeadline(t.ctx, deadline)
	} else {
		sessionCtx, cancel = context.WithCancel(t.ctx)
	}
	serverSession, err := t.server.Connect(sessionCtx, serverTransport, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect MCP server session: %w", err)
	}
	clientConnection, err := clientTransport.Connect(ctx)
	if err != nil {
		cancel()
		_ = serverSession.Close()
		return nil, fmt.Errorf("connect MCP client transport: %w", err)
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		cancel()
		_ = clientConnection.Close()
		_ = serverSession.Close()
		return nil, errRestartableTransportClosed
	}
	t.nextID++
	session := &restartableMCPSession{
		owner:         t,
		id:            t.nextID,
		ctx:           sessionCtx,
		cancel:        cancel,
		client:        clientConnection,
		serverSession: serverSession,
		done:          make(chan struct{}),
	}
	t.sessions[session.id] = session
	t.mu.Unlock()

	t.workers.Add(1)
	go func() {
		defer t.workers.Done()
		waitErr := serverSession.Wait()
		if !session.closing.Load() && t.ctx.Err() == nil {
			t.reportFailure(fmt.Errorf("MCP server session ended: %w", normalizeSessionError(waitErr)))
		}
		session.close(false)
	}()

	return &restartableMCPConnection{session: session, base: clientConnection}, nil
}

func normalizeSessionError(err error) error {
	if err == nil {
		return errors.New("connection closed")
	}
	return err
}

func (t *restartableMCPTransport) reportFailure(err error) {
	if err == nil {
		return
	}
	select {
	case t.failure <- err:
	default:
	}
}

func (t *restartableMCPTransport) Failures() <-chan error {
	return t.failure
}

func (t *restartableMCPTransport) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		sessions := make([]*restartableMCPSession, 0, len(t.sessions))
		for _, session := range t.sessions {
			sessions = append(sessions, session)
		}
		t.mu.Unlock()

		t.cancel()
		for _, session := range sessions {
			session.close(true)
		}
	})
	return nil
}

func (t *restartableMCPTransport) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		t.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *restartableMCPTransport) liveWorkers() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

func (t *restartableMCPTransport) removeSession(id uint64) {
	t.mu.Lock()
	delete(t.sessions, id)
	t.mu.Unlock()
}

func (s *restartableMCPSession) close(intentional bool) {
	if intentional {
		s.closing.Store(true)
	}
	s.closeOnce.Do(func() {
		s.closing.Store(true)
		s.cancel()
		if s.client != nil {
			_ = s.client.Close()
		}
		if s.serverSession != nil {
			_ = s.serverSession.Close()
		}
		s.owner.removeSession(s.id)
		close(s.done)
	})
}

func (c *restartableMCPConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	return c.base.Read(ctx)
}

func (c *restartableMCPConnection) Write(ctx context.Context, message jsonrpc.Message) error {
	return c.base.Write(ctx, message)
}

func (c *restartableMCPConnection) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	c.session.close(true)
	return nil
}

func (c *restartableMCPConnection) SessionID() string {
	if c == nil || c.base == nil {
		return ""
	}
	return c.base.SessionID()
}

var _ mcp.Transport = (*restartableMCPTransport)(nil)
var _ mcp.Connection = (*restartableMCPConnection)(nil)
