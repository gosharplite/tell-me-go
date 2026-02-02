// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

type mockEventBus struct {
	events []events.Event
}

func (m *mockEventBus) Publish(e events.Event) {
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(f func(events.Event)) {}

type mockProcessor struct {
	res ProcessResult
}

func (m *mockProcessor) Process(ctx context.Context, turn *Turn) ProcessResult {
	return m.res
}

func TestWithStreaming(t *testing.T) {
	bus := &mockEventBus{}
	mw := WithStreaming(bus)
	next := &mockProcessor{res: ProcessResult{NextPhase: PhaseComplete}}

	ctx := context.Background()
	turn := &Turn{
		State: &TurnState{Phase: PhaseInference},
	}

	mw(next).Process(ctx, turn)

	if turn.StreamHandler == nil {
		t.Fatal("Expected StreamHandler to be set")
	}

	stream := make(chan *llm.Content, 1)
	turn.StreamHandler(ctx, stream)

	if len(bus.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(bus.events))
	}
	if _, ok := bus.events[0].(events.ResponseStreamEvent); !ok {
		t.Errorf("Expected ResponseStreamEvent, got %T", bus.events[0])
	}
}

func TestWithStatusReporter(t *testing.T) {
	bus := &mockEventBus{}
	mw := WithStatusReporter(bus, nil, "", "", nil)
	next := &mockProcessor{res: ProcessResult{NextPhase: PhaseComplete}}

	cs := NewContextStrategy(nil, nil)
	cm := &ContextManager{Strategy: cs}

	tests := []struct {
		name       string
		phase      TurnPhase
		wantEvents int
	}{
		{"Refining phase", PhaseRefining, 1},
		{"Persisting phase", PhasePersisting, 1},
		{"Other phase", PhaseExecuting, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.events = nil
			turn := &Turn{
				State:      &TurnState{Phase: tt.phase},
				CtxManager: cm,
				Clock:      realClock{},
			}
			mw(next).Process(context.Background(), turn)
			if len(bus.events) != tt.wantEvents {
				t.Errorf("Expected %d events, got %d", tt.wantEvents, len(bus.events))
			}
		})
	}
}

func TestWithMetrics(t *testing.T) {
	bus := &mockEventBus{}
	mw := WithMetrics(bus)
	next := &mockProcessor{res: ProcessResult{NextPhase: PhaseComplete}}

	tests := []struct {
		name       string
		phase      TurnPhase
		hasMetrics bool
		wantEvents int
	}{
		{"Persisting with metrics", PhasePersisting, true, 1},
		{"Persisting without metrics", PhasePersisting, false, 0},
		{"Other phase with metrics", PhaseExecuting, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.events = nil
			state := &TurnState{Phase: tt.phase}
			if tt.hasMetrics {
				state.Metrics = &llm.Metrics{}
			}
			turn := &Turn{
				State: state,
			}
			mw(next).Process(context.Background(), turn)
			if len(bus.events) != tt.wantEvents {
				t.Errorf("Expected %d events, got %d", tt.wantEvents, len(bus.events))
			}
		})
	}
}
