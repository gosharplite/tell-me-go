// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

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
	Mu       sync.RWMutex
	Contents []*llm.Content
	Backup   []*llm.Content
	Resolver llm.AssetResolver

	AddContentFunc  func(ctx context.Context, content *llm.Content) error
	SetContentsFunc func(ctx context.Context, contents []*llm.Content) error
}

func (m *mockHistoryManager) GetTotalEntries() int {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	return len(m.Contents)
}

func (m *mockHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.Mu.RLock()
	defer m.Mu.RUnlock()

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
	m.Mu.Lock()
	defer m.Mu.Unlock()
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
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Contents = append(m.Contents, llm.CloneContent(content))
	return nil
}

func (m *mockHistoryManager) GetResolver() llm.AssetResolver {
	return m.Resolver
}

func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
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
	m.Mu.Lock()
	defer m.Mu.Unlock()

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
	return MockTicker{CVal: m.After(d)}
}
func (m *mockClock) Jitter(base float64) float64 { return base }

type MockTicker struct {
	CVal <-chan time.Time
}

func (m MockTicker) C() <-chan time.Time { return m.CVal }
func (m MockTicker) Stop()               {}

type mockHook struct {
	BeforeCalled int
	AfterCalled  int
	TransCalled  int
}

func (h *mockHook) BeforeTurn(turn *turn)                              { h.BeforeCalled++ }
func (h *mockHook) AfterTurn(turn *turn, err error)                    { h.AfterCalled++ }
func (h *mockHook) OnPhaseTransition(from, to turnPhase, s *turnState) { h.TransCalled++ }

type mockRetryPolicy struct {
	ShouldRetryCalled bool
	Delay             time.Duration
	Retry             bool
}

func (m *mockRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool) {
	m.ShouldRetryCalled = true
	return m.Delay, m.Retry
}

type mockEngineCostTracker struct {
	Mu               sync.Mutex
	AccumulatedCount int
}

func (m *mockEngineCostTracker) CalculateCost(mt llm.Metrics) float64 {
	return 0.05
}

func (m *mockEngineCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.AccumulatedCount++
	return 0.05
}

func (m *mockEngineCostTracker) Accumulate(mt llm.Metrics) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.AccumulatedCount++
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
	Gw       *mockGateway
	Reg      *mockToolRegistry
	Bus      *events.SimpleEventBus
	Cm       *session.ContextManager
	HManager ports.HistoryManager
}

func setupTurnEngineTest(t *testing.T) *testTurnEnv {
	t.Helper()
	reg := &mockToolRegistry{}
	// Use synchronous event bus for deterministic test results
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	t.Cleanup(func() { bus.Shutdown(context.Background()) })

	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	hManager := &mockHistoryManager{}
	cm := newTestContextManager(strategy, hManager, bus)
	gw := &mockGateway{}

	// Default prompt in history
	_ = hManager.AddContent(context.Background(), &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "prompt"}}})

	return &testTurnEnv{
		Gw:       gw,
		Reg:      reg,
		Bus:      bus,
		Cm:       cm,
		HManager: hManager,
	}
}

// costCapturer captures usage metrics and turn status events for assertions.
type costCapturer struct {
	Mu           sync.Mutex
	Bus          events.EventBus
	TurnCosts    []float64
	LastTaskCost float64
}

func newCostCapturer(bus events.EventBus) *costCapturer {
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

func (c *costCapturer) Reset() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.TurnCosts = nil
	c.LastTaskCost = 0
}

func (c *costCapturer) AssertTaskCost(t *testing.T, expected float64) {
	t.Helper()
	_ = c.Bus.Flush(context.Background())
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if fmt.Sprintf("%.6f", c.LastTaskCost) != fmt.Sprintf("%.6f", expected) {
		t.Errorf("expected task cost %f, got %f", expected, c.LastTaskCost)
	}
}

func (c *costCapturer) AssertTurnCosts(t *testing.T, expected []float64) {
	t.Helper()
	_ = c.Bus.Flush(context.Background())
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if len(c.TurnCosts) != len(expected) {
		t.Errorf("expected %d turn costs, got %d", len(expected), len(c.TurnCosts))
		return
	}
	for i, v := range expected {
		if fmt.Sprintf("%.6f", c.TurnCosts[i]) != fmt.Sprintf("%.6f", v) {
			t.Errorf("turn %d: expected cost %f, got %f", i, v, c.TurnCosts[i])
		}
	}
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
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			content := &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
			if hasTools && phase == phaseInference {
				content.Parts = []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test"}}}
			}
			return content, &llm.Metrics{}, nil
		},
	}
	reg := &mockToolRegistry{}
	counter := &mockTokenCounter{}
	turn := &turn{
		State: &turnState{
			HasToolCalls: hasTools,
			RetryCount:   0,
			LastError:    &agentError{Category: llm.ErrTransient, Message: "err"},
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
	if phase == phaseRefining || phase == phaseGuard {
		turn.CtxManager.Pipeline = session.NewContextPipeline()
	}
	return turn
}

func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}

func (m *mockHistoryManager) GetFilePath() string { return "" }

type mockTokenCounter struct {
	Tokens int
}

func (m *mockTokenCounter) Count(contents []*llm.Content) int {
	return m.Tokens
}

func (m *mockTokenCounter) CountTokens(text string) int {
	return m.Tokens
}

type mockGateway struct {
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	SendChatFn   func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *mockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
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
	PublishErr error
}

func (m *mockEventBusFail) Publish(ctx context.Context, e events.Event) error {
	return m.PublishErr
}
func (m *mockEventBusFail) Subscribe(f func(context.Context, events.Event)) {}
func (m *mockEventBusFail) Shutdown(ctx context.Context) error              { return nil }
func (m *mockEventBusFail) Flush(ctx context.Context) error                 { return nil }
func (m *mockEventBusFail) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }

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

func (m *mockHistoryManager) Sync(ctx context.Context) error {
	return nil
}

// engineOption extensions for testing
func withEngineMiddleware(m ...turnMiddleware) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.middleware = append(e.middleware, m...)
	}
}

func withEngineProcessor(phase turnPhase, p turnProcessor) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.processors[phase] = p
	}
}

func withEngineHook(h turnHook) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.hooks = append(e.hooks, h)
	}
}

func withEngineRetryPolicy(p retryPolicy) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.retryPolicy = p
	}
}

type mockTransformer struct {
	TransformFunc func(ctx context.Context, req *ports.ContextRequest) error
}

func (m *mockTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	return m.TransformFunc(ctx, req)
}
func (m *mockTransformer) Priority() int { return 10 }
