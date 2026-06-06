// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

func TestMockEventBusFail_Publish_NilError(t *testing.T) {
	t.Parallel()

	m := &MockEventBusFail{PublishErr: nil}
	err := m.Publish(context.Background(), events.TurnStarted{Turn: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockEventBusFail_Publish_WithError(t *testing.T) {
	t.Parallel()

	want := errors.New("publish failed")
	m := &MockEventBusFail{PublishErr: want}
	err := m.Publish(context.Background(), events.TurnStarted{Turn: 1})
	if !errors.Is(err, want) {
		t.Fatalf("got error %v; want %v", err, want)
	}
}

func TestMockEventBusFail_Subscribe_NoOp(t *testing.T) {
	t.Parallel()

	m := &MockEventBusFail{}
	// Must not panic.
	m.Subscribe(func(ctx context.Context, ev events.Event) {})
}

func TestMockEventBusFail_Shutdown_ReturnsNil(t *testing.T) {
	t.Parallel()

	m := &MockEventBusFail{}
	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockEventBusFail_Flush_ReturnsNil(t *testing.T) {
	t.Parallel()

	m := &MockEventBusFail{}
	err := m.Flush(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockEventBusFail_Listen_Canceled(t *testing.T) {
	t.Parallel()

	m := &MockEventBusFail{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.Listen(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got error %v; want context.Canceled", err)
	}
}

func TestMockEventBusFail_WaitStarted_NoOp(t *testing.T) {
	t.Parallel()

	m := &MockEventBusFail{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.WaitStarted()
	}()

	select {
	case <-done:
		// Expected: WaitStarted returned immediately.
	case <-time.After(time.Second):
		t.Fatal("WaitStarted blocked unexpectedly")
	}
}
