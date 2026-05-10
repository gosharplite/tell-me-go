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
// When used with a panicking subscriber, notifySubscriber catches the
// subscriber panic first, then calls event.Type() in its recover block,
// triggering a second panic that propagates to startSubscriberLoop's recover.
type panickingTypeEvent struct{}

func (e panickingTypeEvent) Type() string { panic("event Type() panic") }

// TestStartSubscriberLoop_PanicRecovery exercises the recover() path in
// startSubscriberLoop where a dynamically-added subscriber causes a panic
// chain that escapes notifySubscriber's recovery and reaches the
// startSubscriberLoop safety net.
//
// The chain works as follows:
//  1. subscriber.Handle panics → caught by notifySubscriber's recover
//  2. notifySubscriber's recover calls event.Type() → second panic (Type panics)
//  3. second panic escapes notifySubscriber → caught by startSubscriberLoop's recover
func TestStartSubscriberLoop_PanicRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

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
	// This triggers the chain: subscriber.Handle panics → notifySubscriber
	// catches it → event.Type() panics in the recover block → startSubscriberLoop catches.
	bus.mu.RLock()
	w := bus.globalSubscribers[0]
	bus.mu.RUnlock()
	w.ch <- panickingTypeEvent{}

	// Give the goroutine time to panic and recover.
	time.Sleep(100 * time.Millisecond)

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
