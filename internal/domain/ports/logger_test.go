// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// testEvent is a minimal Event implementation for testing.
type testEvent struct{}

func (e testEvent) Type() string { return "testEvent" }

var _ events.Event = testEvent{}

func TestNoOpLogger(t *testing.T) {
	t.Parallel()
	l := &NoOpLogger{}
	// Verify no panics — these are intentionally no-op
	l.Error("test %s", "arg")
	l.Warn("test %s", "arg")
	l.Info("test %s", "arg")
	l.Debug("test %s", "arg")
}

func TestNoOpTurnsLogger_HandleEvent(t *testing.T) {
	t.Parallel()
	l := &NoOpTurnsLogger{}
	ctx := context.Background()
	// Verify no panic
	l.HandleEvent(ctx, testEvent{})
}

func TestNoOpTurnsLogger_Listen(t *testing.T) {
	t.Parallel()
	l := &NoOpTurnsLogger{}
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- l.Listen(ctx) }()

	// Verify Listen blocks
	select {
	case <-errCh:
		t.Fatal("Listen returned before cancellation")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Listen did not return after cancellation")
	}
}

func TestNoOpTurnsLogger_Close(t *testing.T) {
	t.Parallel()
	l := &NoOpTurnsLogger{}
	if err := l.Close(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
