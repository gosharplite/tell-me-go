// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// spyEventBus implements events.EventBus and records published events for strict sequence assertion.
type spyEventBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (s *spyEventBus) Publish(ctx context.Context, e events.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *spyEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (s *spyEventBus) Shutdown(ctx context.Context) error                { return nil }
func (s *spyEventBus) Flush(ctx context.Context) error                   { return nil }

// Mock implementations for the facade's domain dependencies.

type seqMockContextPrep struct {
	prepareFunc    func(ctx context.Context, turn int) ([]*llm.Content, int, error)
	addContentFunc func(ctx context.Context, content *llm.Content) error
}

func (m *seqMockContextPrep) Prepare(ctx context.Context, turn int) ([]*llm.Content, int, error) {
	if m.prepareFunc != nil {
		return m.prepareFunc(ctx, turn)
	}
	return nil, 0, nil
}

func (m *seqMockContextPrep) AddContent(ctx context.Context, content *llm.Content) error {
	if m.addContentFunc != nil {
		return m.addContentFunc(ctx, content)
	}
	return nil
}

type seqMockLLMCoord struct {
	generateFunc func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *seqMockLLMCoord) Generate(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, history, tools, resolver)
	}
	return &llm.Content{}, &llm.Metrics{}, nil
}

type seqMockMonitor struct {
	trackUsageFunc    func(ctx context.Context, metrics *llm.Metrics) (float64, error)
	recordErrorFunc   func(ctx context.Context, err error)
	getStatusDataFunc func(ctx context.Context) StatusData
}

func (m *seqMockMonitor) TrackUsage(ctx context.Context, metrics *llm.Metrics) (float64, error) {
	if m.trackUsageFunc != nil {
		return m.trackUsageFunc(ctx, metrics)
	}
	return 0, nil
}

func (m *seqMockMonitor) RecordError(ctx context.Context, err error) {
	if m.recordErrorFunc != nil {
		m.recordErrorFunc(ctx, err)
	}
}

func (m *seqMockMonitor) GetStatusData(ctx context.Context) StatusData {
	if m.getStatusDataFunc != nil {
		return m.getStatusDataFunc(ctx)
	}
	return StatusData{}
}

type seqMockExecution struct {
	executeFunc func(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error)
}

func (m *seqMockExecution) Execute(ctx context.Context, content *llm.Content, turn int, maxTurns int) (*llm.Content, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, content, turn, maxTurns)
	}
	return nil, nil
}

type seqMockRegistry struct {
	tools.Registry
}

func (m *seqMockRegistry) GetDeclarations() []*tools.ToolDeclaration { return nil }

type seqMockHistory struct {
	ports.HistoryManager
}

func (m *seqMockHistory) GetTotalEntries() int           { return 0 }
func (m *seqMockHistory) GetResolver() llm.AssetResolver { return nil }
func (m *seqMockHistory) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

func TestChatterFacade_StrictEventSequence(t *testing.T) {
	// 1. Setup Spy EventBus
	spyBus := &spyEventBus{}

	// 2. Setup Happy Path Mocks
	ctxPrep := &seqMockContextPrep{
		prepareFunc: func(ctx context.Context, turn int) ([]*llm.Content, int, error) {
			return []*llm.Content{}, 100, nil
		},
		addContentFunc: func(ctx context.Context, content *llm.Content) error {
			return nil
		},
	}

	llmCoord := &seqMockLLMCoord{
		generateFunc: func(ctx context.Context, history []*llm.Content, tpl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "assistant",
				Parts: []*llm.Part{{Text: "Hello! I am an AI assistant."}},
			}, &llm.Metrics{PromptTokens: 50, ResponseTokens: 10}, nil
		},
	}

	monitor := &seqMockMonitor{
		trackUsageFunc: func(ctx context.Context, metrics *llm.Metrics) (float64, error) {
			return 0.01, nil
		},
		getStatusDataFunc: func(ctx context.Context) StatusData {
			return StatusData{
				Cost:         0.1,
				DailyCost:    1.0,
				TotalModel:   10,
				TotalHistory: 5,
				TotalOutput:  2,
			}
		},
	}

	execution := &seqMockExecution{}
	registry := &seqMockRegistry{}
	history := &seqMockHistory{}

	// 3. Initialize Facade using constructor and injection options
	facade := NewChatterFacade(
		WithEventBus(spyBus),
		WithContextPrep(ctxPrep),
		WithLLMCoord(llmCoord),
		WithMonitor(monitor),
		WithExecution(execution),
		WithRegistry(registry),
		WithHistory(history),
	)

	// 4. Execute a single successful turn using Chat()
	ctx := context.Background()
	session := &ports.Session{}
	err := facade.Chat(ctx, session, "Hello")

	if err != nil {
		t.Fatalf("Chat failed unexpectedly: %v", err)
	}

	// 5. Assert EXACT sequence of events
	expectedSequence := []struct {
		name     string
		validate func(e events.Event) bool
	}{
		{
			name: "events.StatusUpdate (Starting chat...)",
			validate: func(e events.Event) bool {
				su, ok := e.(events.StatusUpdate)
				return ok && su.Message == "Starting chat..."
			},
		},
		{
			name: "events.TurnStarted",
			validate: func(e events.Event) bool {
				_, ok := e.(events.TurnStarted)
				return ok
			},
		},
		{
			name: "events.TurnStatusEvent (Pre-call, IsPostCall == false)",
			validate: func(e events.Event) bool {
				ts, ok := e.(events.TurnStatusEvent)
				return ok && !ts.Status.IsPostCall
			},
		},
		{
			name: "events.TurnStatusEvent (Post-call, IsPostCall == true)",
			validate: func(e events.Event) bool {
				ts, ok := e.(events.TurnStatusEvent)
				return ok && ts.Status.IsPostCall
			},
		},
		{
			name: "events.TraceEvent",
			validate: func(e events.Event) bool {
				_, ok := e.(events.TraceEvent)
				return ok
			},
		},
	}

	// Fail if extra or missing events
	if len(spyBus.events) != len(expectedSequence) {
		t.Errorf("Event count mismatch: expected %d events, got %d", len(expectedSequence), len(spyBus.events))
		for i, e := range spyBus.events {
			t.Logf("Actual Event %d: %T %+v", i, e, e)
		}
		return
	}

	// Validate each event in order
	for i, expected := range expectedSequence {
		actual := spyBus.events[i]
		if !expected.validate(actual) {
			t.Errorf("Event %d validation failed (%s).\nGot: %T %+v", i, expected.name, actual, actual)
		}
	}
}
