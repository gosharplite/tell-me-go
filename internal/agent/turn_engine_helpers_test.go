// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
)

// MockGateway implements llm.LLMGateway for testing.
type MockGateway struct {
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error))
}

func (m *MockGateway) Generate(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	return m.GenerateFunc(ctx, input, t, resolver)
}

func (m *MockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *MockGateway) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, nil
}

func (m *MockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *MockGateway) RefreshAuth() error { return nil }

func (m *MockGateway) SetSystemInstructions(instr string) {}

// MockRegistry implements ToolRegistry for testing.
type MockRegistry struct {
	Declarations []*tools.ToolDeclaration
}

func (m *MockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Declarations
}

func (m *MockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) {}

func (m *MockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *MockRegistry) IsSerial(name string) bool      { return false }
func (m *MockRegistry) IsLongRunning(name string) bool { return false }

// MockExecutor implements IToolExecutor for testing.
type MockExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *MockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	return m.ExecuteFunc(ctx, respContent, turn, maxToolTurns)
}

// MockStore implements history.Store for testing.
type MockStore struct {
	LoadFunc   func(ctx context.Context) ([]*llm.Content, error)
	SaveFunc   func(ctx context.Context, contents []*llm.Content) error
	AppendFunc func(ctx context.Context, content *llm.Content) error
}

func (m *MockStore) Load(ctx context.Context) ([]*llm.Content, error) { return m.LoadFunc(ctx) }
func (m *MockStore) Save(ctx context.Context, contents []*llm.Content) error {
	return m.SaveFunc(ctx, contents)
}
func (m *MockStore) Append(ctx context.Context, content *llm.Content) error {
	return m.AppendFunc(ctx, content)
}

// MockClock for deterministic tests
type MockClock struct {
	CurrentTime time.Time
}

func (m *MockClock) Now() time.Time { return m.CurrentTime }
func (m *MockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- m.CurrentTime
	return ch
}

type mockHook struct {
	beforeCalled int
	afterCalled  int
	transCalled  int
}

func (h *mockHook) BeforeTurn(turn *turn)                              { h.beforeCalled++ }
func (h *mockHook) AfterTurn(turn *turn, err error)                    { h.afterCalled++ }
func (h *mockHook) OnPhaseTransition(from, to turnPhase, s *turnState) { h.transCalled++ }

type mockRetryPolicy struct {
	shouldRetryCalled bool
	delay             time.Duration
	retry             bool
}

func (m *mockRetryPolicy) ShouldRetry(err error, attempt int) (time.Duration, bool) {
	m.shouldRetryCalled = true
	return m.delay, m.retry
}

type mockEngineCostTracker struct {
	mu               sync.Mutex
	accumulatedCount int
}

func (m *mockEngineCostTracker) CalculateCost(mt llm.Metrics) float64 {
	return 0.05
}

func (m *mockEngineCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accumulatedCount++
	return 0.05
}

func (m *mockEngineCostTracker) Accumulate(mt llm.Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accumulatedCount++
}

func (m *mockEngineCostTracker) GetTotalCost(ctx context.Context) float64 {
	return 0
}

func (m *mockEngineCostTracker) GetDailyCost(ctx context.Context) float64 {
	return 0
}

func (m *mockEngineCostTracker) GetStats(ctx context.Context) (domain_pricing.UsageStats, float64) {
	return domain_pricing.UsageStats{}, 0
}

func (m *mockEngineCostTracker) Warmup() {}

func minimalPipeline() *orchestration.ContextPipeline {
	return orchestration.NewContextPipeline()
}

func newTestContextManager(s *orchestration.ContextStrategy, h services.HistoryManager, bus events.EventBus) *orchestration.ContextManager {
	cm := orchestration.NewContextManager(s, h, bus, nil)
	cm.Pipeline = minimalPipeline()
	return cm
}

// testTurnEnv holds the standard environment for TurnEngine tests.
type testTurnEnv struct {
	gw       *MockGateway
	reg      *MockRegistry
	bus      *events.SimpleEventBus
	cm       *orchestration.ContextManager
	hManager services.HistoryManager
}

func setupTurnEngineTest(t *testing.T) *testTurnEnv {
	t.Helper()
	reg := &MockRegistry{}
	bus := &events.SimpleEventBus{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), bus)
	hManager := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	hManager.SetStore(&MockStore{
		AppendFunc: func(ctx context.Context, content *llm.Content) error { return nil },
		LoadFunc:   func(ctx context.Context) ([]*llm.Content, error) { return nil, nil },
		SaveFunc:   func(ctx context.Context, contents []*llm.Content) error { return nil },
	})
	cm := newTestContextManager(strategy, hManager, bus)
	gw := &MockGateway{}

	// Default prompt in history
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	return &testTurnEnv{
		gw:       gw,
		reg:      reg,
		bus:      bus,
		cm:       cm,
		hManager: hManager,
	}
}

// costCapturer captures usage metrics and turn status events for assertions.
type costCapturer struct {
	mu           sync.Mutex
	turnCosts    []float64
	lastTaskCost float64
}

func newCostCapturer(bus events.EventBus) *costCapturer {
	c := &costCapturer{}
	bus.Subscribe(func(ev events.Event) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if um, ok := ev.(events.UsageMetricsEvent); ok {
			if um.Metrics != nil {
				c.turnCosts = append(c.turnCosts, um.Metrics.Cost)
			}
		}
		if ts, ok := ev.(events.TurnStatusEvent); ok {
			c.lastTaskCost = ts.Status.TaskCost
		}
	})
	return c
}

func (c *costCapturer) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.turnCosts = nil
	c.lastTaskCost = 0
}

func (c *costCapturer) assertTaskCost(t *testing.T, expected float64) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if fmt.Sprintf("%.6f", c.lastTaskCost) != fmt.Sprintf("%.6f", expected) {
		t.Errorf("expected task cost %f, got %f", expected, c.lastTaskCost)
	}
}

func (c *costCapturer) assertTurnCosts(t *testing.T, expected []float64) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.turnCosts) != len(expected) {
		t.Errorf("expected %d turn costs, got %d", len(expected), len(c.turnCosts))
		return
	}
	for i, v := range expected {
		if fmt.Sprintf("%.6f", c.turnCosts[i]) != fmt.Sprintf("%.6f", v) {
			t.Errorf("turn %d: expected cost %f, got %f", i, v, c.turnCosts[i])
		}
	}
}

func closedChan(content *llm.Content) <-chan *llm.Content {
	ch := make(chan *llm.Content, 1)
	if content != nil {
		ch <- content
	}
	close(ch)
	return ch
}

func createProcessorForPhase(phase turnPhase) turnProcessor {
	switch phase {
	case phaseRefining:
		return &ContextRefiner{}
	case phaseInference:
		return &InferenceStep{}
	case phaseExecuting:
		return &ExecutionStep{}
	case phasePersisting:
		return &PersistenceStep{}
	case phaseRecovering:
		return &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
	}
	return nil
}

func setupTransitionTurn(hasTools bool, phase turnPhase) *turn {
	mockGw := &MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			return closedChan(nil), func() (*llm.Content, *llm.Metrics, error) {
				content := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
				if hasTools && phase == phaseInference {
					content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
				}
				return content, &llm.Metrics{}, nil
			}
		},
	}
	reg := &MockRegistry{}
	turn := &turn{
		State: &turnState{
			HasToolCalls: hasTools,
			RetryCount:   0,
			LastError:    &AgentError{Category: llm.ErrTransient, Message: "err"},
			Metadata: &orchestration.Metadata{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			},
		},
		CtxManager: &orchestration.ContextManager{
			History:  history.NewManager(""),
			Strategy: orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), nil),
		},
		Gateway: mockGw,
		Executor: &MockExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test"}}}}, nil
			},
		},
		Registry: reg,
		Clock:    &realClock{},
	}
	if phase == phaseRefining {
		turn.CtxManager.Pipeline = orchestration.NewContextPipeline()
	}
	return turn
}
