// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator/orchestratortest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
)

type localMockEventBus struct {
	events []events.Event
}

func (m *localMockEventBus) Publish(ctx context.Context, e events.Event) error {
	m.events = append(m.events, e)
	return nil
}

func (m *localMockEventBus) Subscribe(f func(context.Context, events.Event)) {}
func (m *localMockEventBus) Shutdown(ctx context.Context) error              { return nil }
func (m *localMockEventBus) Flush(ctx context.Context) error                 { return nil }
func (m *localMockEventBus) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }

type localMockProcessor struct {
	called bool
}

func (m *localMockProcessor) Process(ctx context.Context, Turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
	m.called = true
	return orchestrator.ProcessResult{}, nil
}

func TestWithStatusReporter(t *testing.T) {
	t.Parallel()
	bus := &localMockEventBus{}
	engine := orchestrator.NewEngine(nil, nil, nil, nil, bus, nil)
	mw := engine.WithStatusReporter()
	next := &localMockProcessor{}

	strategy := session.NewContextStrategy(&testutil.MockTokenCounter{})
	cm := orchestratortest.NewTestContextManager(strategy, &testutil.MockHistoryManager{}, bus)
	Turn := &orchestrator.Turn{
		State:      &orchestrator.TurnState{Phase: orchestrator.PhaseInference},
		CtxManager: cm,
		Clock:      &testutil.MockClock{},
	}

	_, _ = mw(next).Process(context.Background(), Turn)

	if !next.called {
		t.Error("Next processor was not called")
	}
	if len(bus.events) == 0 {
		t.Error("No events published")
	}
}

func TestWithMetrics(t *testing.T) {
	t.Parallel()
	bus := &localMockEventBus{}
	engine := orchestrator.NewEngine(nil, nil, nil, nil, bus, nil)
	mw := engine.WithMetrics()
	next := &localMockProcessor{}

	Turn := &orchestrator.Turn{
		State: &orchestrator.TurnState{Phase: orchestrator.PhaseInference, Metrics: &llm.Metrics{}},
	}

	_, _ = mw(next).Process(context.Background(), Turn)

	if !next.called {
		t.Error("Next processor was not called")
	}
}
