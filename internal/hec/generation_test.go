package hec

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tunnelclient "github.com/openai/tunnel-client"
)

type fakeTunnelRuntime struct {
	ready chan struct{}
	done  chan os.Signal

	start func(context.Context) error
	stop  func(context.Context) error
}

func newFakeTunnelRuntime() *fakeTunnelRuntime {
	return &fakeTunnelRuntime{ready: make(chan struct{}), done: make(chan os.Signal)}
}

func (f *fakeTunnelRuntime) Start(ctx context.Context) error {
	if f.start != nil {
		return f.start(ctx)
	}
	return nil
}

func (f *fakeTunnelRuntime) Ready() <-chan struct{} { return f.ready }
func (f *fakeTunnelRuntime) Done() <-chan os.Signal { return f.done }

func (f *fakeTunnelRuntime) Stop(ctx context.Context) error {
	if f.stop != nil {
		return f.stop(ctx)
	}
	return nil
}

func testGenerationManager(factory tunnelRuntimeFactory) *generationManager {
	manager := newGenerationManager(tunnelclient.Config{}, NewDispatcher(), factory)
	manager.readyTimeout = 100 * time.Millisecond
	manager.stopTimeout = 50 * time.Millisecond
	manager.cleanupTimeout = 200 * time.Millisecond
	manager.jitter = func(time.Duration) time.Duration { return 0 }
	return manager
}

func TestGenerationReadinessSuccessAndRootShutdown(t *testing.T) {
	runtime := newFakeTunnelRuntime()
	close(runtime.ready)
	var started atomic.Bool
	var stopped atomic.Bool
	runtime.start = func(context.Context) error {
		started.Store(true)
		return nil
	}
	runtime.stop = func(context.Context) error {
		stopped.Store(true)
		return nil
	}
	manager := testGenerationManager(func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error) {
		return runtime, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, generationErr, cleanupErr := manager.runGeneration(ctx)
	if generationErr != nil || cleanupErr != nil {
		t.Fatalf("generationErr=%v cleanupErr=%v", generationErr, cleanupErr)
	}
	if !started.Load() || !stopped.Load() {
		t.Fatalf("started=%v stopped=%v", started.Load(), stopped.Load())
	}
}

func TestGenerationReadinessTimeout(t *testing.T) {
	runtime := newFakeTunnelRuntime()
	manager := testGenerationManager(func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error) {
		return runtime, nil
	})
	manager.readyTimeout = 15 * time.Millisecond
	_, generationErr, cleanupErr := manager.runGeneration(context.Background())
	if generationErr == nil || !errors.Is(generationErr, context.DeadlineExceeded) {
		t.Fatalf("generation error = %v, want readiness deadline", generationErr)
	}
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestGenerationTunnelStopsBeforeReady(t *testing.T) {
	runtime := newFakeTunnelRuntime()
	close(runtime.done)
	manager := testGenerationManager(func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error) {
		return runtime, nil
	})
	_, generationErr, cleanupErr := manager.runGeneration(context.Background())
	if generationErr == nil {
		t.Fatal("expected stop-before-ready error")
	}
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestGenerationTunnelStopsAfterReady(t *testing.T) {
	runtime := newFakeTunnelRuntime()
	close(runtime.ready)
	manager := testGenerationManager(func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error) {
		return runtime, nil
	})
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(runtime.done)
	}()
	_, generationErr, cleanupErr := manager.runGeneration(context.Background())
	if generationErr == nil {
		t.Fatal("expected stop-after-ready error")
	}
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestGenerationForcedMCPConnectionClosure(t *testing.T) {
	runtime := newFakeTunnelRuntime()
	close(runtime.ready)
	transportReady := make(chan *restartableMCPTransport, 1)
	manager := testGenerationManager(func(_ tunnelclient.Config, transport mcp.Transport) (tunnelRuntime, error) {
		transportReady <- transport.(*restartableMCPTransport)
		return runtime, nil
	})
	go func() {
		transport := <-transportReady
		connection, err := transport.Connect(context.Background())
		if err != nil {
			return
		}
		_ = connection
		transport.mu.Lock()
		var session *restartableMCPSession
		for _, candidate := range transport.sessions {
			session = candidate
		}
		transport.mu.Unlock()
		if session != nil {
			_ = session.serverSession.Close()
		}
	}()
	_, generationErr, cleanupErr := manager.runGeneration(context.Background())
	if generationErr == nil {
		t.Fatal("forced MCP closure did not fail the generation")
	}
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
}

func TestGenerationReplacementWaitsForZeroWorkersAndNeverOverlaps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var factoryCalls atomic.Int32
	var active atomic.Int32
	var maximum atomic.Int32
	var previous *restartableMCPTransport
	var mu sync.Mutex

	manager := testGenerationManager(func(_ tunnelclient.Config, transport mcp.Transport) (tunnelRuntime, error) {
		currentTransport := transport.(*restartableMCPTransport)
		call := factoryCalls.Add(1)
		mu.Lock()
		if previous != nil && previous.liveWorkers() != 0 {
			mu.Unlock()
			t.Fatalf("replacement created with %d old workers", previous.liveWorkers())
		}
		previous = currentTransport
		mu.Unlock()

		runtime := newFakeTunnelRuntime()
		close(runtime.ready)
		runtime.start = func(context.Context) error {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			return nil
		}
		runtime.stop = func(context.Context) error {
			active.Add(-1)
			return nil
		}
		if call == 1 {
			if _, err := currentTransport.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			close(runtime.done)
		} else {
			cancel()
		}
		return runtime, nil
	})
	if err := manager.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if factoryCalls.Load() < 2 {
		t.Fatalf("factory calls = %d, want replacement", factoryCalls.Load())
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum active generations = %d, want 1", maximum.Load())
	}
}

func TestGenerationCleanupTimeoutIsFatalAndPreventsReplacement(t *testing.T) {
	var factoryCalls atomic.Int32
	manager := testGenerationManager(func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error) {
		factoryCalls.Add(1)
		runtime := newFakeTunnelRuntime()
		close(runtime.done)
		runtime.stop = func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}
		return runtime, nil
	})
	manager.stopTimeout = 10 * time.Millisecond
	manager.cleanupTimeout = 20 * time.Millisecond
	err := manager.Run(context.Background())
	if err == nil {
		t.Fatal("expected fatal cleanup timeout")
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("factory calls = %d, replacement overlapped fatal cleanup", factoryCalls.Load())
	}
}

func TestGenerationPermanentFactoryErrorDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	manager := testGenerationManager(func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error) {
		calls.Add(1)
		return nil, errors.New("invalid startup configuration")
	})
	if err := manager.Run(context.Background()); err == nil {
		t.Fatal("expected permanent startup error")
	}
	if calls.Load() != 1 {
		t.Fatalf("factory calls = %d, want 1", calls.Load())
	}
}

func TestGenerationBackoffIsExactBoundedAndCancellable(t *testing.T) {
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second}
	if len(reconnectBackoffSteps) != len(want) {
		t.Fatalf("backoff length = %d", len(reconnectBackoffSteps))
	}
	for index, duration := range want {
		if reconnectBackoffSteps[index] != duration {
			t.Fatalf("backoff[%d] = %s, want %s", index, reconnectBackoffSteps[index], duration)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := sleepContext(ctx, 10*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("canceled backoff took %s", elapsed)
	}
}

func TestGenerationManagerOneHundredTurnoversWithoutOverlap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const turnovers = 100
	var calls atomic.Int32
	var active atomic.Int32
	var maximum atomic.Int32
	var previous *restartableMCPTransport
	var mu sync.Mutex

	manager := testGenerationManager(func(_ tunnelclient.Config, transport mcp.Transport) (tunnelRuntime, error) {
		currentTransport := transport.(*restartableMCPTransport)
		call := calls.Add(1)
		mu.Lock()
		if previous != nil && previous.liveWorkers() != 0 {
			mu.Unlock()
			t.Fatalf("generation %d created with %d previous workers", call, previous.liveWorkers())
		}
		previous = currentTransport
		mu.Unlock()

		runtime := newFakeTunnelRuntime()
		close(runtime.ready)
		runtime.start = func(context.Context) error {
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			return nil
		}
		runtime.stop = func(context.Context) error {
			active.Add(-1)
			return nil
		}
		if call <= turnovers {
			if _, err := currentTransport.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			close(runtime.done)
		} else {
			cancel()
		}
		return runtime, nil
	})
	if err := manager.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != turnovers+1 {
		t.Fatalf("generation count = %d, want %d", got, turnovers+1)
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum active generations = %d, want 1", got)
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("active generations after shutdown = %d", got)
	}
}
