// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// mockExecutor implements toolExecutor for testing.
type mockExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *mockExecutor) Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
	return m.ExecuteFunc(ctx, respContent, turn, maxToolTurns)
}

// mockHistoryManager implements ports.HistoryManager for testing.
type mockHistoryManager struct {
	mu       sync.RWMutex
	Contents []*llm.Content
	Backup   []*llm.Content
	Resolver llm.AssetResolver

	AddContentFunc  func(ctx context.Context, content *llm.Content) error
	SetContentsFunc func(ctx context.Context, contents []*llm.Content) error
}

func (m *mockHistoryManager) GetTotalEntries() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Contents)
}

func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.Contents)
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx > total {
		startIdx = total
	}
	if endIdx == -1 || endIdx > total {
		endIdx = total
	}
	if endIdx < startIdx {
		return []*llm.Content{}, nil
	}

	window := m.Contents[startIdx:endIdx]
	res := make([]*llm.Content, len(window))
	for i, c := range window {
		res[i] = llm.CloneContent(c)
	}
	return res, nil
}

func (m *mockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.SetContentsFunc != nil {
		return m.SetContentsFunc(ctx, contents)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Contents = contents
	return nil
}

func (m *mockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}

func (m *mockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	if m.AddContentFunc != nil {
		return m.AddContentFunc(ctx, content)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Contents = append(m.Contents, llm.CloneContent(content))
	return nil
}

func (m *mockHistoryManager) GetResolver() llm.AssetResolver {
	return m.Resolver
}

func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	startIdx := turnIndex * 2
	if startIdx < 0 || startIdx+1 >= len(m.Contents) {
		return fmt.Errorf("invalid turn index")
	}
	m.Contents[startIdx].Pinned = pinned
	m.Contents[startIdx+1].Pinned = pinned
	return nil
}

func (m *mockHistoryManager) Save(ctx context.Context) error {
	return nil
}

func (m *mockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	originalLen := len(m.Contents)
	if originalLen == 0 || turns <= 0 {
		return 0, originalLen / 2, originalLen, nil
	}

	removeMsgs := turns * 2
	var actualRemoved int
	if removeMsgs >= originalLen {
		actualRemoved = originalLen / 2
		m.Contents = nil
	} else {
		actualRemoved = turns
		m.Contents = m.Contents[:originalLen-removeMsgs]
	}

	remainingMsgs := len(m.Contents)
	remainingTurns := remainingMsgs / 2

	return actualRemoved, remainingTurns, remainingMsgs, nil
}

// mockClock for deterministic tests
type mockClock struct {
	CurrentTime time.Time
}

func (m *mockClock) Now() time.Time { return m.CurrentTime }
func (m *mockClock) Sleep(d time.Duration) {
	m.CurrentTime = m.CurrentTime.Add(d)
}
func (m *mockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	m.CurrentTime = m.CurrentTime.Add(d)
	ch <- m.CurrentTime
	return ch
}
func (m *mockClock) Jitter(base float64) float64 { return base }

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

func (m *mockRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int) (time.Duration, bool) {
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

func newTestContextManager(s *orchestration.ContextStrategy, h ports.HistoryManager, bus events.EventBus) *orchestration.ContextManager {
	cm := orchestration.NewContextManager(s, h, bus, nil)
	cm.Pipeline = minimalPipeline()
	return cm
}

// testTurnEnv holds the standard environment for TurnEngine tests.
type testTurnEnv struct {
	gw       *mockGateway
	reg      *mockToolRegistry
	bus      *events.SimpleEventBus
	cm       *orchestration.ContextManager
	hManager ports.HistoryManager
}

func setupTurnEngineTest(t *testing.T) *testTurnEnv {
	t.Helper()
	reg := &mockToolRegistry{}
	bus := events.NewSimpleEventBus()
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), bus)
	hManager := &mockHistoryManager{}
	cm := newTestContextManager(strategy, hManager, bus)
	gw := &mockGateway{}

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
	bus          events.EventBus
	turnCosts    []float64
	lastTaskCost float64
}

func newCostCapturer(bus events.EventBus) *costCapturer {
	c := &costCapturer{bus: bus}
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
	_ = c.bus.Flush(context.Background())
	c.mu.Lock()
	defer c.mu.Unlock()
	if fmt.Sprintf("%.6f", c.lastTaskCost) != fmt.Sprintf("%.6f", expected) {
		t.Errorf("expected task cost %f, got %f", expected, c.lastTaskCost)
	}
}

func (c *costCapturer) assertTurnCosts(t *testing.T, expected []float64) {
	t.Helper()
	_ = c.bus.Flush(context.Background())
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
	case phaseGuard:
		return &guardStep{}
	case phaseRefining:
		return &contextRefiner{}
	case phaseInference:
		return &inferenceStep{}
	case phaseExecuting:
		return &executionStep{}
	case phasePersisting:
		return &persistenceStep{}
	case phaseRecovering:
		return &recoveryStep{Policy: &defaultRetryPolicy{MaxRetries: 3}}
	}
	return nil
}

func setupTransitionTurn(hasTools bool, phase turnPhase) *turn {
	mockGw := &mockGateway{
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
	reg := &mockToolRegistry{}
	counter := &mockTokenCounter{}
	turn := &turn{
		State: &turnState{
			HasToolCalls: hasTools,
			RetryCount:   0,
			LastError:    &agentError{Category: llm.ErrTransient, Message: "err"},
			Metadata: &orchestration.Metadata{
				History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
			},
		},
		CtxManager: &orchestration.ContextManager{
			History:  &mockHistoryManager{},
			Strategy: orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), nil),
		},
		Gateway: mockGw,
		executor: &mockExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
				return &llm.Content{Role: "user", Parts: []*llm.Part{{FunctionResponse: &llm.FunctionResponse{Name: "test"}}}}, nil
			},
		},
		Registry:     reg,
		TokenCounter: counter,
		Clock:        &clock.RealClock{},
	}
	if phase == phaseRefining || phase == phaseGuard {
		turn.CtxManager.Pipeline = orchestration.NewContextPipeline()
	}
	return turn
}

func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}
