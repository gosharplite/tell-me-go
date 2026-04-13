// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// Exported for external tests

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

func (e *Engine) WithMetrics() TurnMiddleware {
	return e.withMetrics()
}

func (e *Engine) WithStatusReporter() TurnMiddleware {
	return e.withStatusReporter()
}

func (p *InferenceStep) InvokeModel(ctx context.Context, turn *Turn) (*llm.Content, *llm.Metrics, error) {
	return p.invokeModel(ctx, turn)
}

func IsTransient(err error) bool {
	return isTransient(err)
}

func IsFatal(err error) bool {
	return isFatal(err)
}

func (p *ExecutionStep) ValidatePayloadLimits(ctx context.Context, turn *Turn) {
	p.validatePayloadLimits(ctx, turn)
}

func (p *InferenceStep) HasToolCalls(content *llm.Content) bool {
	return p.hasToolCalls(content)
}

func ValidatePayloadLimits(p *ExecutionStep, ctx context.Context, turn *Turn) {
	p.validatePayloadLimits(ctx, turn)
}

func ExecutePhase(e *Engine, ctx context.Context, turn *Turn) (ProcessResult, error) {
	return e.ExecutePhase(ctx, turn)
}

func CreateProcessorForPhase(phase TurnPhase) TurnProcessor {
	switch phase {
	case PhaseGuard:
		return &GuardStep{}
	case PhaseRefining:
		return &ContextRefiner{}
	case PhaseInference:
		return &InferenceStep{}
	case PhaseExecuting:
		return &ExecutionStep{}
	case PhasePersisting:
		return &PersistenceStep{}
	case PhaseRecovering:
		return &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
	}
	return nil
}

func SetupTransitionTurn(hasTools bool, phase TurnPhase) *Turn {
	mockGw := &testutil.MockGateway{}
	mockGw.GenerateFunc = func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		content := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
		if hasTools && phase == PhaseInference {
			content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
		}
		return content, &llm.Metrics{}, nil
	}
	reg := &testutil.MockToolRegistry{}
	counter := &testutil.MockTokenCounter{}
	turn := &Turn{
		State: &TurnState{
			HasToolCalls: hasTools,
			RetryCount:   0,
			LastError:    &AgentError{Category: llm.ErrTransient, Message: "err"},
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
	if phase == PhaseRefining || phase == PhaseGuard {
		turn.CtxManager.Pipeline = session.NewContextPipeline()
	}
	return turn
}

func (e *Engine) Processors() map[TurnPhase]TurnProcessor {
	return e.processors
}

// MockHook is a mock implementation of TurnHook for testing.
type MockHook struct {
	BeforeCalled int
	AfterCalled  int
	TransCalled  int
}

func (m *MockHook) BeforeTurn(turn *Turn) {
	m.BeforeCalled++
}

func (m *MockHook) AfterTurn(turn *Turn, err error) {
	m.AfterCalled++
}

func (m *MockHook) OnPhaseTransition(from, to TurnPhase, state *TurnState) {
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

// NewCostCapturer creates a new CostCapturer.
func NewCostCapturer(bus events.EventBus) *CostCapturer {
	c := &CostCapturer{Bus: bus}
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

// CostCapturer captures usage metrics and Turn status events for assertions.
type CostCapturer struct {
	Mu           sync.Mutex
	Bus          events.EventBus
	TurnCosts    []float64
	LastTaskCost float64
}

func (c *CostCapturer) Reset() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.TurnCosts = nil
	c.LastTaskCost = 0
}

func (c *CostCapturer) AssertTaskCost(t interface{ Errorf(string, ...any) }, expected float64) {
	_ = c.Bus.Flush(context.Background())
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if fmt.Sprintf("%.6f", c.LastTaskCost) != fmt.Sprintf("%.6f", expected) {
		t.Errorf("expected task cost %f, got %f", expected, c.LastTaskCost)
	}
}

func (c *CostCapturer) AssertTurnCosts(t interface{ Errorf(string, ...any) }, expected []float64) {
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

func TruncateOversizedResponse(toolResponse *llm.Content, estimatedTokens int, instruction string) {
	truncateOversizedResponse(toolResponse, estimatedTokens, instruction)
}
