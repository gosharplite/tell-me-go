// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

type mockEventBusFail struct {
	publishErr  error
	shutdownErr error
}

func (m *mockEventBusFail) Publish(ctx context.Context, e events.Event) error { return m.publishErr }
func (m *mockEventBusFail) Subscribe(sub func(context.Context, events.Event)) {}
func (m *mockEventBusFail) Shutdown(ctx context.Context) error                { return m.shutdownErr }
func (m *mockEventBusFail) Flush(ctx context.Context) error                   { return nil }
func (m *mockEventBusFail) Listen(ctx context.Context) error                  { <-ctx.Done(); return ctx.Err() }
func (m *mockEventBusFail) WaitStarted()                                      {}

type mockEvent struct{}

func (e mockEvent) Type() string { return "MockEvent" }

func TestAgent_Emit_PublishFailureLogsError(t *testing.T) {
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	badBus := &mockEventBusFail{publishErr: errors.New("simulated publish failure")}
	a := &Agent{events: badBus, logger: testLogger}
	a.emit(context.Background(), mockEvent{})

	if !strings.Contains(buf.String(), "event_publish_failed") {
		t.Errorf("Expected event_publish_failed log, got: %s", buf.String())
	}
}

func TestAgent_Emit_ErrBusNotInitialized_NoLog(t *testing.T) {
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	badBus := &mockEventBusFail{publishErr: events.ErrBusNotInitialized}
	a := &Agent{events: badBus, logger: testLogger}
	a.emit(context.Background(), mockEvent{})

	if strings.Contains(buf.String(), "event_publish_failed") {
		t.Errorf("Expected no log for ErrBusNotInitialized, got: %s", buf.String())
	}
}

func TestAgent_Shutdown_ErrBusNotInitialized(t *testing.T) {
	badBus := &mockEventBusFail{shutdownErr: events.ErrBusNotInitialized}
	a := &Agent{events: badBus}

	err := a.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Expected Shutdown to return nil on ErrBusNotInitialized, got: %v", err)
	}
}
