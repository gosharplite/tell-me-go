// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"testing"
	"time"
)

func TestSimpleEventBus_DynamicSubscription(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	bus := NewSimpleEventBus(ctx)

	// Start the listener in a background goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- bus.Listen(ctx)
	}()

	// Wait for the listener to be fully initialized
	bus.WaitStarted()

	received := make(chan Event, 1)
	bus.Subscribe(func(ctx context.Context, e Event) {
		received <- e
	})

	// Publish an event AFTER starting and subscribing
	err := bus.Publish(ctx, StatusUpdate{Message: "test"})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case <-received:
		// Pass
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Dynamic subscriber never received event")
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Listen() returned unexpected error: %v", err)
		}
	}
}

func TestSimpleEventBus_DynamicSubscription_Specific(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	bus := NewSimpleEventBus(ctx)

	go func() {
		_ = bus.Listen(ctx)
	}()
	bus.WaitStarted()

	received := make(chan Event, 1)
	bus.SubscribeSubscriber("StatusUpdate", &funcSubscriber{f: func(ctx context.Context, e Event) {
		received <- e
	}})

	err := bus.Publish(ctx, StatusUpdate{Message: "test"})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case <-received:
		// Pass
	case <-time.After(500 * time.Millisecond):
		t.Errorf("Dynamic subscriber for specific event never received event")
	}
}
