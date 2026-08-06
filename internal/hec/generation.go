package hec

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tunnelclient "github.com/openai/tunnel-client"
)

const sustainedHealthyGeneration = 30 * time.Second

var reconnectBackoffSteps = [...]time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
}

type permanentGenerationError struct {
	err error
}

func (e *permanentGenerationError) Error() string { return e.err.Error() }
func (e *permanentGenerationError) Unwrap() error { return e.err }

type tunnelRuntime interface {
	Start(context.Context) error
	Ready() <-chan struct{}
	Done() <-chan os.Signal
	Stop(context.Context) error
}

type tunnelRuntimeFactory func(tunnelclient.Config, mcp.Transport) (tunnelRuntime, error)

type generationManager struct {
	config     tunnelclient.Config
	dispatcher *Dispatcher
	factory    tunnelRuntimeFactory

	readyTimeout   time.Duration
	stopTimeout    time.Duration
	cleanupTimeout time.Duration
	healthyReset   time.Duration
	now            func() time.Time
	jitter         func(time.Duration) time.Duration
}

func newGenerationManager(config tunnelclient.Config, dispatcher *Dispatcher, factory tunnelRuntimeFactory) *generationManager {
	if dispatcher == nil {
		dispatcher = NewDispatcher()
	}
	if factory == nil {
		factory = func(config tunnelclient.Config, transport mcp.Transport) (tunnelRuntime, error) {
			return tunnelclient.New(config, transport)
		}
	}
	return &generationManager{
		config:         config,
		dispatcher:     dispatcher,
		factory:        factory,
		readyTimeout:   GenerationReadyTimeout,
		stopTimeout:    TunnelStopTimeout,
		cleanupTimeout: GenerationCleanupTimeout,
		healthyReset:   sustainedHealthyGeneration,
		now:            time.Now,
		jitter: func(base time.Duration) time.Duration {
			if base <= 0 {
				return 0
			}
			maximumExtra := base / 4
			if maximumExtra <= 0 {
				return base
			}
			result := base + time.Duration(rand.Int64N(int64(maximumExtra)+1))
			if result > 10*time.Second {
				return 10 * time.Second
			}
			return result
		},
	}
}

func (m *generationManager) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	backoffIndex := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		healthyDuration, generationErr, cleanupErr := m.runGeneration(ctx)
		if cleanupErr != nil {
			return fmt.Errorf("tunnel generation cleanup failed: %w", cleanupErr)
		}
		if ctx.Err() != nil {
			return nil
		}
		if generationErr == nil {
			return nil
		}
		var permanent *permanentGenerationError
		if errors.As(generationErr, &permanent) {
			return permanent
		}

		if healthyDuration >= m.healthyReset {
			backoffIndex = 0
		}
		base := reconnectBackoffSteps[backoffIndex]
		if backoffIndex < len(reconnectBackoffSteps)-1 {
			backoffIndex++
		}
		delay := m.jitter(base)
		if err := sleepContext(ctx, delay); err != nil {
			return nil
		}
	}
}

func (m *generationManager) runGeneration(root context.Context) (time.Duration, error, error) {
	generationCtx, cancelGeneration := context.WithCancel(root)
	server := NewMCPServer(m.dispatcher)
	transport := newRestartableMCPTransport(generationCtx, server)

	client, err := m.factory(m.config, transport)
	if err != nil {
		cancelGeneration()
		_ = transport.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), m.cleanupTimeout)
		defer cleanupCancel()
		waitErr := transport.Wait(cleanupCtx)
		if waitErr != nil {
			return 0, &permanentGenerationError{err: fmt.Errorf("create tunnel client: %w", err)}, waitErr
		}
		return 0, &permanentGenerationError{err: fmt.Errorf("create tunnel client: %w", err)}, nil
	}
	if err := client.Start(generationCtx); err != nil {
		cleanupErr := m.cleanupGeneration(cancelGeneration, client, transport)
		return 0, fmt.Errorf("start tunnel client: %w", err), cleanupErr
	}

	readyCtx, cancelReady := context.WithTimeout(generationCtx, m.readyTimeout)
	var generationErr error
	var readyAt time.Time
	select {
	case <-client.Ready():
		readyAt = m.now()
	case err := <-transport.Failures():
		generationErr = err
	case <-client.Done():
		generationErr = errors.New("tunnel client stopped before becoming ready")
	case <-readyCtx.Done():
		if root.Err() == nil {
			generationErr = fmt.Errorf("tunnel generation readiness: %w", readyCtx.Err())
		}
	}
	cancelReady()

	if generationErr == nil && !readyAt.IsZero() {
		fmt.Fprintln(os.Stderr, "HEC tunnel generation connected")
		select {
		case <-root.Done():
		case err := <-transport.Failures():
			generationErr = err
		case <-client.Done():
			if root.Err() == nil {
				generationErr = errors.New("tunnel client stopped")
			}
		}
	}

	healthyDuration := time.Duration(0)
	if !readyAt.IsZero() {
		healthyDuration = m.now().Sub(readyAt)
	}
	cleanupErr := m.cleanupGeneration(cancelGeneration, client, transport)
	return healthyDuration, generationErr, cleanupErr
}

func (m *generationManager) cleanupGeneration(cancelGeneration context.CancelFunc, client tunnelRuntime, transport *restartableMCPTransport) error {
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), m.cleanupTimeout)
	defer cancelCleanup()

	cancelGeneration()
	stopCtx, cancelStop := context.WithTimeout(cleanupCtx, m.stopTimeout)
	stopErr := client.Stop(stopCtx)
	cancelStop()
	_ = transport.Close()
	waitErr := transport.Wait(cleanupCtx)

	if waitErr != nil {
		return waitErr
	}
	if transport.liveWorkers() != 0 {
		return fmt.Errorf("old tunnel generation retained %d MCP workers", transport.liveWorkers())
	}
	if stopErr != nil {
		if errors.Is(stopErr, context.DeadlineExceeded) || errors.Is(stopErr, context.Canceled) {
			return stopErr
		}
		fmt.Fprintln(os.Stderr, "HEC tunnel generation stopped with a recoverable error after full cleanup")
	}
	return nil
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
