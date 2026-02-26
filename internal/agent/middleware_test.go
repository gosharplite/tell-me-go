// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

type mockEventBus struct {
	events []events.Event
}

func (m *mockEventBus) Publish(e events.Event) {
	m.events = append(m.events, e)
}

func (m *mockEventBus) Subscribe(f func(events.Event)) {}

func (m *mockEventBus) Shutdown(ctx context.Context) error { return nil }

func (m *mockEventBus) Flush(ctx context.Context) error { return nil }

type mockProcessor struct {
	res processResult
	err error
}

func (m *mockProcessor) process(ctx context.Context, turn *turn) (processResult, error) {
	return m.res, m.err
}

func TestWithStreaming(t *testing.T) {
	bus := &mockEventBus{}
	e := &turnEngine{events: bus}
	mw := e.WithStreaming()
	next := &mockProcessor{res: processResult{NextPhase: phaseComplete}}

	ctx := context.Background()
	turn := &turn{
		State: &turnState{Phase: phaseInference},
		Clock: &mockClock{},
	}

	_, _ = mw(next).process(ctx, turn)

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
	e := &turnEngine{events: bus}
	mw := e.WithStatusReporter()
	next := &mockProcessor{res: processResult{NextPhase: phaseComplete}}

	cs := orchestration.NewContextStrategy(nil, nil)
	h := history.NewManager(infrapersistence.NewOSFileSystem(), "", "")
	cm := &orchestration.ContextManager{Strategy: cs, History: h}

	tests := []struct {
		name       string
		phase      turnPhase
		wantEvents int
	}{
		{"Refining phase", phaseRefining, 1},
		{"Persisting phase", phasePersisting, 1},
		{"Other phase", phaseExecuting, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.events = nil
			turn := &turn{
				State:      &turnState{Phase: tt.phase},
				CtxManager: cm,
				Clock:      clock.RealClock{},
			}
			_, _ = mw(next).process(context.Background(), turn)
			if len(bus.events) != tt.wantEvents {
				t.Errorf("Expected %d events, got %d", tt.wantEvents, len(bus.events))
			}
		})
	}
}

func TestWithMetrics(t *testing.T) {
	bus := &mockEventBus{}
	e := &turnEngine{events: bus}
	mw := e.WithMetrics()
	next := &mockProcessor{res: processResult{NextPhase: phaseComplete}}

	tests := []struct {
		name       string
		phase      turnPhase
		hasMetrics bool
		wantEvents int
	}{
		{"Persisting with metrics", phasePersisting, true, 1},
		{"Persisting without metrics", phasePersisting, false, 0},
		{"Other phase with metrics", phaseExecuting, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.events = nil
			state := &turnState{Phase: tt.phase}
			if tt.hasMetrics {
				state.Metrics = &llm.Metrics{}
			}
			turn := &turn{
				State: state,
				Clock: &mockClock{},
			}
			_, _ = mw(next).process(context.Background(), turn)
			if len(bus.events) != tt.wantEvents {
				t.Errorf("Expected %d events, got %d", tt.wantEvents, len(bus.events))
			}
		})
	}
}

func setupLoopDetectorTest() (turnMiddleware, *mockProcessor, *turn) {
	mw := withLoopDetector()
	next := &mockProcessor{res: processResult{NextPhase: phaseComplete}}
	state := &turnState{
		Phase:                phaseInference,
		RecentResponseHashes: make([]string, 0),
		Response: &llm.Content{
			Parts: []*llm.Part{{Text: "initial"}},
		},
	}
	return mw, next, &turn{State: state, Clock: clock.RealClock{}}
}

func fillBuffer(t *testing.T, mw turnMiddleware, next *mockProcessor, turn *turn, count int) {
	for i := 0; i < count; i++ {
		turn.State.Response.Parts[0].Text = "response " + string(rune(i))
		_, err := mw(next).process(context.Background(), turn)
		assert.NoError(t, err)
	}
}

func TestWithLoopDetector_Rotation(t *testing.T) {
	t.Run("Fill Buffer", func(t *testing.T) {
		mw, next, turn := setupLoopDetectorTest()
		fillBuffer(t, mw, next, turn, domain_config.DefaultMaxLoopRepetitions)
		assert.Len(t, turn.State.RecentResponseHashes, domain_config.DefaultMaxLoopRepetitions)
	})

	t.Run("Trigger Rotation", func(t *testing.T) {
		mw, next, turn := setupLoopDetectorTest()
		fillBuffer(t, mw, next, turn, domain_config.DefaultMaxLoopRepetitions)
		oldestHash := turn.State.RecentResponseHashes[0]

		turn.State.Response.Parts[0].Text = "unique"
		_, err := mw(next).process(context.Background(), turn)
		assert.NoError(t, err)

		assert.Len(t, turn.State.RecentResponseHashes, domain_config.DefaultMaxLoopRepetitions)
		assert.NotContains(t, turn.State.RecentResponseHashes, oldestHash)
	})

	t.Run("High Volume", func(t *testing.T) {
		mw, next, turn := setupLoopDetectorTest()
		fillBuffer(t, mw, next, turn, 100)
		assert.Len(t, turn.State.RecentResponseHashes, domain_config.DefaultMaxLoopRepetitions)
	})
}
