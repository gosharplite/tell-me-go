// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestratortest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// TestTurnEnv holds the standard environment for TurnEngine tests.
type TestTurnEnv struct {
	Gw       *testutil.MockGateway
	Reg      *testutil.MockToolRegistry
	Bus      *events.SimpleEventBus
	Cm       *session.ContextManager
	HManager ports.HistoryManager
}

func SetupTurnEngineTest(t interface {
	Helper()
	Cleanup(func())
}) *TestTurnEnv {
	t.Helper()
	reg := &testutil.MockToolRegistry{}
	// Use synchronous event bus for deterministic test results
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &testutil.MockHistoryManager{}
	cm := NewTestContextManager(strategy, hManager, bus)
	gw := &testutil.MockGateway{}

	// Default prompt in history
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	return &TestTurnEnv{
		Gw:       gw,
		Reg:      reg,
		Bus:      bus,
		Cm:       cm,
		HManager: hManager,
	}
}

func NewTestContextManager(s *session.ContextStrategy, h ports.HistoryManager, bus events.EventBus) *session.ContextManager {
	cm := session.NewContextManager(s, h, bus, nil)
	cm.Pipeline = session.NewContextPipeline()
	return cm
}

func CreateProcessorForPhase(phase orchestrator.TurnPhase) orchestrator.TurnProcessor {
	switch phase {
	case orchestrator.PhaseGuard:
		return &orchestrator.GuardStep{}
	case orchestrator.PhaseRefining:
		return &orchestrator.ContextRefiner{}
	case orchestrator.PhaseInference:
		return &orchestrator.InferenceStep{}
	case orchestrator.PhaseExecuting:
		return &orchestrator.ExecutionStep{}
	case orchestrator.PhasePersisting:
		return &orchestrator.PersistenceStep{}
	case orchestrator.PhaseRecovering:
		return &orchestrator.RecoveryStep{Policy: &orchestrator.DefaultRetryPolicy{MaxRetries: 3}}
	}
	return nil
}

func SetupTransitionTurn(hasTools bool, phase orchestrator.TurnPhase) *orchestrator.Turn {
	mockGw := &testutil.MockGateway{}
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		content := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
		if hasTools && phase == orchestrator.PhaseInference {
			content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
		}
		return content, &llm.Metrics{}, nil
	}
	reg := &testutil.MockToolRegistry{}
	counter := &testutil.MockTokenCounter{}
	turn := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			HasToolCalls: hasTools,
			RetryCount:   0,
			LastError:    orchestrator.NewAgentError(llm.ErrTransient, "err", nil),
			Metadata: &session.Metadata{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			},
		},
		CtxManager: &session.ContextManager{
			History:  &testutil.MockHistoryManager{},
			Strategy: session.NewContextStrategy(session.NewHeuristicTokenCounter(reg)),
		},
		Gateway: mockGw,
		Executor: &testutil.MockAgentExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turnIdx int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test"}}}}, nil
			},
		},
		Registry:     reg,
		TokenCounter: counter,
		Clock:        &clock.RealClock{},
	}
	if phase == orchestrator.PhaseRefining || phase == orchestrator.PhaseGuard {
		turn.CtxManager.Pipeline = session.NewContextPipeline()
	}
	return turn
}

// MockHook is a mock implementation of TurnHook for testing.
type MockHook struct {
	BeforeCalled int
	AfterCalled  int
	TransCalled  int
}

func (m *MockHook) BeforeTurn(turn *orchestrator.Turn) {
	m.BeforeCalled++
}

func (m *MockHook) AfterTurn(turn *orchestrator.Turn, err error) {
	m.AfterCalled++
}

func (m *MockHook) OnPhaseTransition(from, to orchestrator.TurnPhase, state *orchestrator.TurnState) {
	m.TransCalled++
}

// MockRetryPolicy is a mock implementation of RetryPolicy for testing.
type MockRetryPolicy struct {
	Retry              bool
	ShouldRetryCalled  bool
	ShouldRetryResults []time.Duration
}

func (m *MockRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int, seenRateLimit bool) (time.Duration, bool) {
	m.ShouldRetryCalled = true
	if attempt < len(m.ShouldRetryResults) {
		return m.ShouldRetryResults[attempt], m.Retry
	}
	return 0, m.Retry
}

// NewCostCapturer creates a new costCapturer.
func NewCostCapturer(bus events.EventBus) *costCapturer {
	c := &costCapturer{Bus: bus}
	bus.Subscribe(func(ctx context.Context, ev events.Event) {
		c.Mu.Lock()
		defer c.Mu.Unlock()
		if um, ok := ev.(events.UsageMetricsEvent); ok {
			if um.Metrics != nil {
				c.TurnCosts = append(c.TurnCosts, um.Metrics.Cost)
			}
		}
		if ts, ok := ev.(events.TurnStatusEvent); ok {
			c.LastTaskCost = ts.Status.TaskCost
		}
	})
	return c
}

// costCapturer captures usage metrics and Turn status events for assertions.
type costCapturer struct {
	Mu           sync.Mutex
	Bus          events.EventBus
	TurnCosts    []float64
	LastTaskCost float64
}

func (c *costCapturer) Reset() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.TurnCosts = nil
	c.LastTaskCost = 0
}

func (c *costCapturer) AssertTaskCost(t interface{ Errorf(string, ...any) }, expected float64) {
	_ = c.Bus.Flush(context.Background())
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if fmt.Sprintf("%.6f", c.LastTaskCost) != fmt.Sprintf("%.6f", expected) {
		t.Errorf("expected task cost %f, got %f", expected, c.LastTaskCost)
	}
}

func (c *costCapturer) AssertTurnCosts(t interface{ Errorf(string, ...any) }, expected []float64) {
	_ = c.Bus.Flush(context.Background())
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if len(c.TurnCosts) != len(expected) {
		t.Errorf("expected %d Turn costs, got %d", len(expected), len(c.TurnCosts))
		return
	}
	for i, v := range expected {
		if fmt.Sprintf("%.6f", c.TurnCosts[i]) != fmt.Sprintf("%.6f", v) {
			t.Errorf("Turn %d: expected cost %f, got %f", i, v, c.TurnCosts[i])
		}
	}
}
