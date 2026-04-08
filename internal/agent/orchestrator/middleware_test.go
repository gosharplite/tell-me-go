// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type mockEventBus struct {
	events []events.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	m.events = append(m.events, e)
	return nil
}

func (m *mockEventBus) Subscribe(f func(context.Context, events.Event)) {}
func (m *mockEventBus) Shutdown(ctx context.Context) error              { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error                 { return nil }
func (m *mockEventBus) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }

type mockProcessor struct {
	called bool
}

func (m *mockProcessor) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	m.called = true
	return ProcessResult{}, nil
}

func TestWithStatusReporter(t *testing.T) {
	t.Parallel()
	bus := &mockEventBus{}
	engine := &Engine{events: bus}
	mw := engine.WithStatusReporter()
	next := &mockProcessor{}

	strategy := session.NewContextStrategy(&mockTokenCounter{})
	cm := newTestContextManager(strategy, &mockHistoryManager{}, bus)
	turn := &Turn{
		State:      &TurnState{Phase: PhaseInference},
		CtxManager: cm,
		Clock:      &mockClock{},
	}

	_, _ = mw(next).Process(context.Background(), turn)

	if !next.called {
		t.Error("Next processor was not called")
	}
	if len(bus.events) == 0 {
		t.Error("No events published")
	}
}

func TestWithMetrics(t *testing.T) {
	t.Parallel()
	bus := &mockEventBus{}
	engine := &Engine{events: bus}
	mw := engine.WithMetrics()
	next := &mockProcessor{}

	turn := &Turn{
		State: &TurnState{Phase: PhaseInference, Metrics: &llm.Metrics{}},
	}

	_, _ = mw(next).Process(context.Background(), turn)

	if !next.called {
		t.Error("Next processor was not called")
	}
}

func TestWithLoopDetector_Rotation(t *testing.T) {
	t.Parallel()
	// This test depends on logic in middleware.go which uses domain_config.DefaultMaxLoopRepetitions
}
