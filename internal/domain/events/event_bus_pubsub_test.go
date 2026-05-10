// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// panicSubscriber is a test helper that panics in Handle.
// Duplicated from events_test.go (package events_test) for white-box access.
type panicSubscriber struct {
	msg string
}

func (s *panicSubscriber) Handle(ctx context.Context, e Event) error {
	panic(s.msg)
}

// panickingTypeEvent is an event whose Type() method panics.
// With Fix 1 (cached event.Type()), notifySubscriber calls Type() before
// sub.Handle, so the panic happens there; the recover catches it and returns
// an error. subscriberLoop then calls event.Type() again in its error-logging
// path, triggering a second panic that propagates to startSubscriberLoop's recover.
type panickingTypeEvent struct{}

func (e panickingTypeEvent) Type() string { panic("event Type() panic") }

// hookHandler wraps a slog.Handler and closes onPanicMsg when it intercepts
// a log message containing the expected substring. Used for deterministic
// synchronization with background goroutines instead of time.Sleep.
type hookHandler struct {
	slog.Handler
	onPanicMsg chan struct{}
}

func (h *hookHandler) Handle(ctx context.Context, r slog.Record) error {
	if strings.Contains(r.Message, "panic in dynamic event bus subscriber loop") {
		select {
		case <-h.onPanicMsg:
			// already closed
		default:
			close(h.onPanicMsg)
		}
	}
	return h.Handler.Handle(ctx, r)
}

// TestStartSubscriberLoop_PanicRecovery exercises the recover() path in
// startSubscriberLoop where a panic escapes both notifySubscriber's recovery
// and subscriberLoop, reaching the startSubscriberLoop safety net.
//
// The chain works as follows:
//  1. notifySubscriber calls event.Type() (cached before defer) → panics
//  2. notifySubscriber's recover catches it, returns an error
//  3. subscriberLoop calls event.Type() again for error logging → second panic
//  4. second panic escapes subscriberLoop → caught by startSubscriberLoop's recover
func TestStartSubscriberLoop_PanicRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	panicCaught := make(chan struct{})
	hook := &hookHandler{Handler: baseHandler, onPanicMsg: panicCaught}
	logger := slog.New(hook)

	bus := NewSimpleEventBus(ctx,
		WithAsync(true),
		WithQueueSize(1),
		WithLogger(logger),
	)

	// Start Listen in background so the bus is "running".
	// This is required for startSubscriberLoop to be called by SubscribeGlobal.
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = bus.Listen(ctx)
	}()
	bus.WaitStarted()

	// Dynamically subscribe a subscriber that panics on Handle.
	// Because the bus is already running, SubscribeGlobal calls startSubscriberLoop.
	bus.SubscribeGlobal(&panicSubscriber{msg: "dynamic panic boom"})

	// Send a panickingTypeEvent directly to the subscriber's channel,
	// bypassing Publish (which would panic on event.Type()).
	// This triggers the chain: notifySubscriber calls event.Type() → panics →
	// recover returns error → subscriberLoop calls event.Type() for logging →
	// second panic → startSubscriberLoop catches.
	bus.mu.RLock()
	w := bus.globalSubscribers[0]
	bus.mu.RUnlock()
	w.ch <- panickingTypeEvent{}

	// Wait for startSubscriberLoop's recover to log the panic.
	select {
	case <-panicCaught:
		// Good — the recover fired and logged the expected message.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for startSubscriberLoop panic recovery log")
	}

	// Shutdown cleanly.
	cancel()
	<-listenDone

	// Verify the panic was logged by startSubscriberLoop's recover().
	output := buf.String()
	if !strings.Contains(output, "panic in dynamic event bus subscriber loop") {
		t.Errorf("expected 'panic in dynamic event bus subscriber loop' in log, got: %s", output)
	}
	if !strings.Contains(output, "event Type() panic") {
		t.Errorf("expected 'event Type() panic' in log, got: %s", output)
	}
}
