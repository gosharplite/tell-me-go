// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
	"pgregory.net/rapid"
)

type rapidEvent struct {
	typeName string
}

func (e rapidEvent) Type() string { return e.typeName }

// propertyBusMachine implements a rapid.StateMachine to test SimpleEventBus.
type propertyBusMachine struct {
	bus           *SimpleEventBus
	ctx           context.Context
	cancel        context.CancelFunc
	listenStarted atomic.Bool
	listenDone    chan struct{}

	published atomic.Int64
	processed atomic.Int64

	isShutdown atomic.Bool
	subMu      sync.Mutex
	numSubs    int
}

// Check verifies invariants before and after each action.
func (m *propertyBusMachine) Check(t *rapid.T) {
	if m.bus == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
		// Use small queue size to stress backpressure and load shedding
		queueSize := rapid.IntRange(1, 20).Draw(t, "queueSize")
		m.bus = NewSimpleEventBus(m.ctx, WithAsync(true), WithQueueSize(queueSize))
		m.listenDone = make(chan struct{})
		m.published.Store(0)
		m.processed.Store(0)
		m.isShutdown.Store(false)
		m.listenStarted.Store(false)
		m.numSubs = 0
	}

	// Invariant: If marked as shutdown, the internal closed flag must be set.
	if m.isShutdown.Load() {
		m.bus.mu.RLock()
		closed := m.bus.closed
		m.bus.mu.RUnlock()
		if !closed {
			t.Fatal("isShutdown is true but bus.closed is false")
		}
	}
}

func (m *propertyBusMachine) cleanup() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.listenStarted.Load() {
		select {
		case <-m.listenDone:
		case <-time.After(100 * time.Millisecond):
		}
	}
	if m.bus != nil {
		_ = m.bus.Shutdown(context.Background())
	}
}

// ActionListen starts the event bus.
func (m *propertyBusMachine) ActionListen(t *rapid.T) {
	if m.isShutdown.Load() || m.listenStarted.Load() {
		return
	}
	m.listenStarted.Store(true)
	go func() {
		defer close(m.listenDone)
		_ = m.bus.Listen(m.ctx)
	}()
}

// ActionSubscribe adds a new subscriber.
func (m *propertyBusMachine) ActionSubscribe(t *rapid.T) {
	if m.isShutdown.Load() || m.listenStarted.Load() {
		return
	}

	m.subMu.Lock()
	m.numSubs++
	m.subMu.Unlock()

	m.bus.Subscribe(func(ctx context.Context, e Event) {
		m.processed.Add(1)
	})
}

// ActionPublish sends an event to the bus.
func (m *propertyBusMachine) ActionPublish(t *rapid.T) {
	eventType := rapid.SampledFrom([]string{"A", "B", "C"}).Draw(t, "eventType")
	m.published.Add(1)

	// Randomly apply short timeouts to test context handling during publish
	publishCtx := m.ctx
	if rapid.Bool().Draw(t, "withTimeout") {
		var cancel context.CancelFunc
		publishCtx, cancel = context.WithTimeout(m.ctx, 1*time.Millisecond)
		defer cancel()
	}

	err := m.bus.Publish(publishCtx, rapidEvent{typeName: eventType})
	if m.isShutdown.Load() {
		// After shutdown, Publish MUST return ErrBusClosed or context error
		if err != nil && err != ErrBusClosed && err != context.Canceled && err != context.DeadlineExceeded {
			t.Fatalf("unexpected error after shutdown: %v", err)
		}
	}
}

// ActionFlush waits for pending events.
func (m *propertyBusMachine) ActionFlush(t *rapid.T) {
	if !m.listenStarted.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := m.bus.Flush(ctx)
	if m.isShutdown.Load() && err != nil && err != ErrBusClosed && err != context.Canceled {
		t.Fatalf("unexpected flush error after shutdown: %v", err)
	}
}

// ActionShutdown stops the event bus.
func (m *propertyBusMachine) ActionShutdown(t *rapid.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = m.bus.Shutdown(ctx)
	m.isShutdown.Store(true)
	m.cancel()
}

// ActionConcurrentOps performs multiple operations in parallel to test thread safety.
func (m *propertyBusMachine) ActionConcurrentOps(t *rapid.T) {
	var wg sync.WaitGroup
	n := rapid.IntRange(2, 8).Draw(t, "concurrency")
	for i := 0; i < n; i++ {
		wg.Add(1)
		op := rapid.SampledFrom([]string{"publish", "flush", "shutdown"}).Draw(t, fmt.Sprintf("op%d", i))
		go func(opName string) {
			defer wg.Done()
			switch opName {
			case "publish":
				_ = m.bus.Publish(m.ctx, rapidEvent{typeName: "concurrent"})
			case "flush":
				if m.listenStarted.Load() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
					_ = m.bus.Flush(ctx)
					cancel()
				}
			case "shutdown":
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				_ = m.bus.Shutdown(ctx)
				m.isShutdown.Store(true)
				m.cancel()
				cancel()
			}
		}(op)
	}
	wg.Wait()
}

// TestSimpleEventBus_PropertyBased validates event bus invariants using generative testing.
func TestSimpleEventBus_PropertyBased(t *testing.T) {
	// Verify no goroutine leaks across iterations
	defer goleak.VerifyNone(t)

	rapid.Check(t, func(t *rapid.T) {
		m := new(propertyBusMachine)
		t.Repeat(rapid.StateMachineActions(m))

		// Final Cleanup and Assertions
		m.cleanup()

		if m.listenStarted.Load() {
			select {
			case <-m.listenDone:
			case <-time.After(1 * time.Second):
				t.Fatal("Listen goroutine did not exit after context cancellation")
			}
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if m.bus != nil {
			_ = m.bus.Shutdown(shutdownCtx)

			// Resource Cleanup Invariant: Ensure all worker goroutines are released
			done := make(chan struct{})
			go func() {
				m.bus.workerWG.Wait()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("workerWG did not reach zero; background goroutines are still active")
			}
		}
	})
}
