// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// mockExecutor implements ToolExecutor for testing.
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

func (m *mockHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
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

func (m *mockClock) Now() time.Time                  { return m.CurrentTime }
func (m *mockClock) Since(t time.Time) time.Duration { return m.CurrentTime.Sub(t) }
func (m *mockClock) Sleep(d time.Duration) {
	m.CurrentTime = m.CurrentTime.Add(d)
}
func (m *mockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	m.CurrentTime = m.CurrentTime.Add(d)
	ch <- m.CurrentTime
	return ch
}
func (m *mockClock) NewTicker(d time.Duration) clock.Ticker {
	return mockTicker{c: m.After(d)}
}
func (m *mockClock) Jitter(base float64) float64 { return base }

type mockTicker struct {
	c <-chan time.Time
}

func (m mockTicker) C() <-chan time.Time { return m.c }
func (m mockTicker) Stop()               {}

type mockHook struct {
	beforeCalled int
	afterCalled  int
	transCalled  int
}

func (h *mockHook) BeforeTurn(turn *Turn)                              { h.beforeCalled++ }
func (h *mockHook) AfterTurn(turn *Turn, err error)                    { h.afterCalled++ }
func (h *mockHook) OnPhaseTransition(from, to TurnPhase, s *TurnState) { h.transCalled++ }

type mockRetryPolicy struct {
	shouldRetryCalled bool
	delay             time.Duration
	retry             bool
}

func (m *mockRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool) {
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

func minimalPipeline() *session.ContextPipeline {
	return session.NewContextPipeline()
}

func newTestContextManager(s *session.ContextStrategy, h ports.HistoryManager, bus events.EventBus) *session.ContextManager {
	cm := session.NewContextManager(s, h, bus, nil)
	cm.Pipeline = minimalPipeline()
	return cm
}

// testTurnEnv holds the standard environment for TurnEngine tests.
type testTurnEnv struct {
	gw       *mockGateway
	reg      *mockToolRegistry
	bus      *events.SimpleEventBus
	cm       *session.ContextManager
	hManager ports.HistoryManager
}

func setupTurnEngineTest(t *testing.T) *testTurnEnv {
	t.Helper()
	reg := &mockToolRegistry{}
	// Use synchronous event bus for deterministic test results
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
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
	bus.Subscribe(func(ctx context.Context, ev events.Event) {
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

func createProcessorForPhase(phase TurnPhase) TurnProcessor {
	switch phase {
	case PhaseGuard:
		return &guardStep{}
	case PhaseRefining:
		return &contextRefiner{}
	case PhaseInference:
		return &inferenceStep{}
	case PhaseExecuting:
		return &executionStep{}
	case PhasePersisting:
		return &persistenceStep{}
	case PhaseRecovering:
		return &recoveryStep{Policy: &defaultRetryPolicy{MaxRetries: 3}}
	}
	return nil
}

func setupTransitionTurn(hasTools bool, phase TurnPhase) *Turn {
	mockGw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			content := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
			if hasTools && phase == PhaseInference {
				content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
			}
			return content, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	counter := &mockTokenCounter{}
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
			History:  &mockHistoryManager{},
			Strategy: session.NewContextStrategy(session.NewHeuristicTokenCounter(reg)),
		},
		Gateway: mockGw,
		Executor: &mockExecutor{
			ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
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

func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}

func (m *mockHistoryManager) GetFilePath() string { return "" }

type mockTokenCounter struct {
	tokens int
}

func (m *mockTokenCounter) Count(contents []*llm.Content) int {
	return m.tokens
}

func (m *mockTokenCounter) CountTokens(text string) int {
	return m.tokens
}

type mockGateway struct {
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	sendChatFn   func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *mockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.sendChatFn != nil {
		return m.sendChatFn(ctx, history, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *mockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return [][]byte{}, nil
}

func (m *mockGateway) RefreshAuth() error { return nil }

type mockToolRegistry struct {
	Declarations []*tools.ToolDeclaration
}

func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Declarations
}

func (m *mockToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) error {
	return m.RegisterToToolkit("core", declaration, implementation)
}

func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterToToolkitWithOptions("core", def, handler, opts)
}

func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockToolRegistry) IsSerial(name string) bool {
	return false
}

func (m *mockToolRegistry) IsLongRunning(name string) bool {
	return false
}

func (m *mockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *mockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.RegisterToToolkitWithOptions(toolkit, def, handler, tools.ToolOptions{})
}

func (m *mockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.Declarations = append(m.Declarations, def)
	return nil
}

func (m *mockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockToolRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}

type mockEventBusFail struct {
	publishErr error
}

func (m *mockEventBusFail) Publish(ctx context.Context, e events.Event) error {
	return m.publishErr
}
func (m *mockEventBusFail) Subscribe(f func(context.Context, events.Event)) {}
func (m *mockEventBusFail) Shutdown(ctx context.Context) error              { return nil }
func (m *mockEventBusFail) Flush(ctx context.Context) error                 { return nil }

type mockSecurityManager struct {
	domain_security.Manager
	AllowAll bool
}

func (m *mockSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (m *mockSecurityManager) TerminalLock()                          {}
func (m *mockSecurityManager) TerminalUnlock()                        {}
func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	return m.AllowAll
}

func (m *mockSecurityManager) Close() error { return nil }
