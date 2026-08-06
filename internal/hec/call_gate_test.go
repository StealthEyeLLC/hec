package hec

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dispatchFunc func(context.Context, CallRequest) Result

func (f dispatchFunc) Dispatch(ctx context.Context, request CallRequest) Result {
	return f(ctx, request)
}

func TestPublicCallGateSerializesCompleteDispatch(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	handler := newPublicCallHandler(dispatchFunc(func(context.Context, CallRequest) Result {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		result := newResult("health")
		result.OK = true
		return result
	}))
	if cap(handler.gate.token) != 1 {
		t.Fatalf("call gate capacity = %d, want 1", cap(handler.gate.token))
	}

	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan Result, 2)
	for range 2 {
		go func() {
			defer wait.Done()
			results <- handler.dispatch(context.Background(), CallRequest{Operation: "health", Args: map[string]any{}})
		}()
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first dispatch did not enter")
	}
	select {
	case <-entered:
		t.Fatal("second dispatch entered while first was active")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	wait.Wait()
	close(results)
	for result := range results {
		if !result.OK {
			t.Fatalf("dispatch result = %#v", result)
		}
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum active dispatch = %d, want 1", got)
	}
}

func TestPublicCallGateAcquisitionRespectsCancellation(t *testing.T) {
	handler := newPublicCallHandler(dispatchFunc(func(context.Context, CallRequest) Result {
		t.Fatal("dispatch should not execute")
		return Result{}
	}))
	if err := handler.gate.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer handler.gate.release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := handler.dispatch(ctx, CallRequest{Operation: "health", Args: map[string]any{}})
	if result.Error == nil || result.Error.Code != "canceled" {
		t.Fatalf("result = %#v, want canceled", result)
	}
}

func TestPublicCallGateAcquisitionTimeout(t *testing.T) {
	if CallGateAcquireTimeout != 10*time.Second {
		t.Fatalf("CallGateAcquireTimeout = %s, want 10s", CallGateAcquireTimeout)
	}
	handler := newPublicCallHandler(dispatchFunc(func(context.Context, CallRequest) Result {
		t.Fatal("dispatch should not execute")
		return Result{}
	}))
	handler.gate.acquireTimeout = 25 * time.Millisecond
	if err := handler.gate.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer handler.gate.release()
	started := time.Now()
	result := handler.dispatch(context.Background(), CallRequest{Operation: "health", Args: map[string]any{}})
	if result.Error == nil || result.Error.Code != "queue_timeout" {
		t.Fatalf("result = %#v, want queue_timeout", result)
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("gate timed out too early after %s", elapsed)
	}
}

func TestPublicCallPanicReleasesGateAndRedactsStack(t *testing.T) {
	var calls atomic.Int32
	handler := newPublicCallHandler(dispatchFunc(func(context.Context, CallRequest) Result {
		if calls.Add(1) == 1 {
			panic("sensitive-marker")
		}
		result := newResult("health")
		result.OK = true
		return result
	}))
	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	}()

	first := handler.dispatch(context.Background(), CallRequest{Operation: "health", Args: map[string]any{}})
	if first.Error == nil || first.Error.Code != "internal_error" {
		t.Fatalf("panic result = %#v", first)
	}
	if strings.Contains(logs.String(), "sensitive-marker") {
		t.Fatalf("panic log leaked recovered value: %q", logs.String())
	}
	second := handler.dispatch(context.Background(), CallRequest{Operation: "health", Args: map[string]any{}})
	if !second.OK {
		t.Fatalf("later dispatch unusable: %#v", second)
	}
}
