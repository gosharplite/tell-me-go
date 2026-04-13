// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

// MockToolRegistry is a mock implementation of tools.Registry for testing.
type MockToolRegistry struct {
	Declarations []*tools.ToolDeclaration
	ToolkitMap   map[string][]*tools.ToolDeclaration
	Handlers     map[string]tools.ToolFunc
	Options      map[string]tools.ToolOptions
	RegisterErr  error
	FailAfter    int
	CallCount    int
}

func NewMockToolRegistry() *MockToolRegistry {
	return &MockToolRegistry{
		ToolkitMap: make(map[string][]*tools.ToolDeclaration),
		Handlers:   make(map[string]tools.ToolFunc),
		Options:    make(map[string]tools.ToolOptions),
	}
}

func (m *MockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Declarations
}

func (m *MockToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) error {
	return m.RegisterToToolkit("core", declaration, implementation)
}

func (m *MockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterToToolkitWithOptions("core", def, handler, opts)
}

func (m *MockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if handler, ok := m.Handlers[name]; ok {
		return handler(ctx, args, hb)
	}
	return tools.ToolResult{}, nil
}

func (m *MockToolRegistry) IsSerial(name string) bool {
	return m.Options[name].Serial
}

func (m *MockToolRegistry) IsLongRunning(name string) bool {
	return m.Options[name].LongRunning
}

func (m *MockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return m.Options[name]
}

func (m *MockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.RegisterToToolkitWithOptions(toolkit, def, handler, tools.ToolOptions{})
}

func (m *MockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.CallCount++
	if m.RegisterErr != nil && (m.FailAfter == 0 || m.CallCount > m.FailAfter) {
		return m.RegisterErr
	}
	m.Declarations = append(m.Declarations, def)
	if m.ToolkitMap == nil {
		m.ToolkitMap = make(map[string][]*tools.ToolDeclaration)
	}
	m.ToolkitMap[toolkit] = append(m.ToolkitMap[toolkit], def)
	if m.Handlers == nil {
		m.Handlers = make(map[string]tools.ToolFunc)
	}
	m.Handlers[def.Name] = handler
	if m.Options == nil {
		m.Options = make(map[string]tools.ToolOptions)
	}
	m.Options[def.Name] = opts
	return nil
}

func (m *MockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.ToolkitMap["core"]
}

func (m *MockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	dedup := make(map[string]*tools.ToolDeclaration)
	for _, d := range m.ToolkitMap["core"] {
		dedup[d.Name] = d
	}
	for _, tk := range toolkits {
		for _, d := range m.ToolkitMap[tk] {
			dedup[d.Name] = d
		}
	}
	res := make([]*tools.ToolDeclaration, 0, len(dedup))
	for _, d := range dedup {
		res = append(res, d)
	}
	return res
}

func (m *MockToolRegistry) ListAvailableToolkits() []string {
	toolkits := make([]string, 0, len(m.ToolkitMap))
	for tk := range m.ToolkitMap {
		toolkits = append(toolkits, tk)
	}
	return toolkits
}

func (m *MockToolRegistry) SetRegisterErr(err error) { m.RegisterErr = err }
func (m *MockToolRegistry) SetFailAfter(n int)       { m.FailAfter = n }

// MockSummarizer is a mock implementation of ports.Summarizer.
type MockSummarizer struct {
	SummarizeFn func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error)
}

func (m *MockSummarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
	if m.SummarizeFn != nil {
		return m.SummarizeFn(ctx, subset, focus)
	}
	return "summary", &llm.Metrics{}, nil
}

func (m *MockSummarizer) SetSummarizeFn(fn func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error)) {
	m.SummarizeFn = fn
}

// MockTokenCounter is a mock implementation of llm.TokenCounter.
type MockTokenCounter struct {
	Tokens int
}

func (m *MockTokenCounter) Count(contents []*llm.Content) int {
	return m.Tokens
}

func (m *MockTokenCounter) SetTokens(n int) {
	m.Tokens = n
}

// MockHistoryManager is a mock implementation of ports.HistoryManager.
type MockHistoryManager struct {
	Mu             sync.RWMutex
	Contents       []*llm.Content
	resolver       llm.AssetResolver
	SetContentsErr error
	GetWindowErr   error
	RollbackErr    error

	AddContentFunc  func(ctx context.Context, content *llm.Content) error
	SetContentsFunc func(ctx context.Context, contents []*llm.Content) error
}

func (m *MockHistoryManager) GetTotalEntries() int {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	return len(m.Contents)
}

func (m *MockHistoryManager) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (m *MockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	if m.GetWindowErr != nil {
		return nil, m.GetWindowErr
	}
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
	cloned := make([]*llm.Content, len(window))
	for i, c := range window {
		cloned[i] = llm.CloneContent(c)
	}
	return cloned, nil
}

func (m *MockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	if m.SetContentsFunc != nil {
		return m.SetContentsFunc(ctx, contents)
	}
	if m.SetContentsErr != nil {
		return m.SetContentsErr
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Contents = contents
	return nil
}

func (m *MockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}

func (m *MockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	if m.AddContentFunc != nil {
		return m.AddContentFunc(ctx, content)
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Contents = append(m.Contents, llm.CloneContent(content))
	return nil
}

func (m *MockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	if index >= 0 && index < len(m.Contents) {
		m.Contents[index].Parts = append(m.Contents[index].Parts, parts...)
	}
	return nil
}

func (m *MockHistoryManager) GetResolver() llm.AssetResolver { return m.resolver }
func (m *MockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
}
func (m *MockHistoryManager) Save(ctx context.Context) error { return nil }
func (m *MockHistoryManager) Sync(ctx context.Context) error { return nil }
func (m *MockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	if m.RollbackErr != nil {
		return 0, 0, 0, m.RollbackErr
	}
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
	return actualRemoved, len(m.Contents) / 2, len(m.Contents), nil
}
func (m *MockHistoryManager) GetFilePath() string       { return "" }
func (m *MockHistoryManager) SetGetWindowErr(err error) { m.GetWindowErr = err }
func (m *MockHistoryManager) SetInternalContents(contents []*llm.Content) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Contents = contents
}

// MockGateway is a mock implementation of llm.LLMGateway.
type MockGateway struct {
	mock.Mock
	GenerateFunc func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	SendChatFn   func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *MockGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *MockGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

func (m *MockGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return [][]byte{}, nil
}

func (m *MockGateway) RefreshAuth() error { return nil }

func (m *MockGateway) SetGenerateFn(fn func(context.Context, []*llm.Content, []*tools.ToolDeclaration, llm.AssetResolver) (*llm.Content, *llm.Metrics, error)) {
	m.GenerateFunc = fn
}

// MockTransformer is a mock implementation of ports.ContextTransformer.
type MockTransformer struct {
	PriorityVal   int
	TransformFunc func(ctx context.Context, req *ports.ContextRequest) error
}

func (m *MockTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if m.TransformFunc != nil {
		return m.TransformFunc(ctx, req)
	}
	return nil
}

func (m *MockTransformer) Priority() int { return m.PriorityVal }

func (m *MockTransformer) SetTransformFn(fn func(context.Context, *ports.ContextRequest) error) {
	m.TransformFunc = fn
}

// MockAgentExecutor is a helper for TurnEngine tests.
type MockAgentExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *MockAgentExecutor) Execute(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, respContent, Turn, maxToolTurns)
	}
	return nil, nil
}

// MockEventBusFail is a mock implementation of events.EventBus that always fails on Publish.
type MockEventBusFail struct {
	PublishErr error
}

func (m *MockEventBusFail) Publish(ctx context.Context, e events.Event) error {
	return m.PublishErr
}
func (m *MockEventBusFail) Subscribe(f func(context.Context, events.Event)) {}
func (m *MockEventBusFail) Shutdown(ctx context.Context) error              { return nil }
func (m *MockEventBusFail) Flush(ctx context.Context) error                 { return nil }
func (m *MockEventBusFail) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }

// ToolBehavior defines the expected behavior of a tool for testing.
type ToolBehavior struct {
	Result  tools.ToolResult
	Err     error
	Delay   time.Duration
	Panic   interface{}
	Serial  bool
	Long    bool
	Observe func() // Callback to signal execution
}

// MockChatter is a mock implementation of ports.Chatter.
type MockChatter struct {
	mock.Mock
}

func (m *MockChatter) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	args := m.Called(ctx, s, prompt)
	return args.Error(0)
}

func (m *MockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	args := m.Called(ctx, toolTurns, historyTokens, historyTurns)
	return args.Error(0)
}

func (m *MockChatter) SetTieredThreshold(ctx context.Context, threshold int) error {
	args := m.Called(ctx, threshold)
	return args.Error(0)
}

func (m *MockChatter) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}

func (m *MockChatter) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockCapturer is a mock implementation of ports.Capturer.
type MockCapturer struct {
	mock.Mock
}

func (m *MockCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *MockCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	callArgs := m.Called(ctx, args, opts)
	return callArgs.String(0), callArgs.Error(1)
}

func (m *MockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}

func (m *MockCapturer) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockCapturer) Warn(msg string)   { m.Called(msg) }
func (m *MockCapturer) Prompt(msg string) { m.Called(msg) }
func (m *MockCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *MockCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

// MockSessionProvider is a mock implementation of ports.SessionProvider.
type MockSessionProvider struct {
	mock.Mock
}

func (m *MockSessionProvider) GetTasks() ports.TaskStore { return nil }
func (m *MockSessionProvider) GetSettings() ports.KVStore {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.KVStore)
}
func (m *MockSessionProvider) GetInfo() ports.SessionInfo {
	args := m.Called()
	return args.Get(0).(ports.SessionInfo)
}
func (m *MockSessionProvider) SetInfo(info ports.SessionInfo) {
	m.Called(info)
}
func (m *MockSessionProvider) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockLLMClient is a flexible mock for testing.
type MockLLMClient struct {
	mock.Mock
	SendChatFn    func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	RefreshAuthFn func() error
}

func (m *MockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.SendChatFn != nil {
		return m.SendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, fmt.Errorf("SendChatFn not implemented")
}

func (m *MockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *MockLLMClient) RefreshAuth() error {
	if m.RefreshAuthFn != nil {
		return m.RefreshAuthFn()
	}
	return nil
}

func (m *MockLLMClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return m.SendChat(ctx, input, tools, resolver)
}

// PanicRegistry is a mock tool registry that can simulate panics.
type PanicRegistry struct {
	tools.Registry
	PanicOnExec bool
	PanicOnGet  bool
	Serial      bool
}

func (r *PanicRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if r.PanicOnGet {
		panic("registry GetDeclarations panic")
	}
	return []*tools.ToolDeclaration{{Name: "any"}}
}

func (r *PanicRegistry) IsSerial(name string) bool {
	return r.Serial
}

func (r *PanicRegistry) IsLongRunning(name string) bool {
	return false
}

func (r *PanicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if r.PanicOnExec {
		panic("registry Execute panic")
	}
	return tools.ToolResult{}, nil
}

func (r *PanicRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: r.IsSerial(name), LongRunning: r.IsLongRunning(name)}
}

func (r *PanicRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (r *PanicRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (r *PanicRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return r.GetDeclarations()
}

func (r *PanicRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return r.GetDeclarations()
}

func (r *PanicRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}

// MockExecutor implements tools.CommandExecutor for testing.
type MockExecutor struct {
	OutputBytes []byte
	Error       error
	CommandName string
	CommandArgs []string
}

// Output records the command and returns the pre-set output and error.
func (m *MockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

// CombinedOutput records the command and returns the pre-set output and error.
func (m *MockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

// LookPath simulates looking for an executable in the path.
func (m *MockExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

func (m *MockHistoryManager) GetContents() []*llm.Content {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	res := make([]*llm.Content, len(m.Contents))
	for i, c := range m.Contents {
		res[i] = llm.CloneContent(c)
	}
	return res
}

func (m *MockHistoryManager) SetSetContentsErr(err error) {
	m.SetContentsErr = err
}

// MockLogger is a mock implementation of tools.ExecutionObserver for testing.
type MockLogger struct {
	CriticalLogs chan string
}

func (m *MockLogger) ExecutionTimedOut(toolID string) {
	if m.CriticalLogs != nil {
		m.CriticalLogs <- "CRITICAL: Tool goroutine permanently leaked: " + toolID
	}
}

func (m *MockLogger) ExecutionCompletedLate(toolID string) {}

// MockCostTracker is a mock implementation of pricing.CostTracker for testing.
type MockCostTracker struct {
	mu               sync.Mutex
	AccumulatedCount int
}

func (m *MockCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccumulatedCount++
	return 0.05
}

func (m *MockCostTracker) Accumulate(mt llm.Metrics) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccumulatedCount++
}

func (m *MockCostTracker) GetTotalCost(ctx context.Context) float64 {
	return 0.05
}

func (m *MockCostTracker) GetDailyCost(ctx context.Context) float64 {
	return 0.05
}

func (m *MockCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	return pricing.UsageStats{}, 0.05
}

func (m *MockCostTracker) Warmup() {}

// MockEngineCostTracker is an alias for MockCostTracker.
type MockEngineCostTracker = MockCostTracker

// MockTurnsLogger is a mock implementation of ports.TurnsLogger for testing.
type MockTurnsLogger struct {
	mock.Mock
}

func (m *MockTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {
	m.Called(ctx, e)
}

func (m *MockTurnsLogger) Listen(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockTurnsLogger) Close() error {
	return m.Called().Error(0)
}

func (m *MockHistoryManager) SetRollbackErr(err error) {
	m.RollbackErr = err
}

func (m *MockTokenCounter) EstimateTokens(contents []*llm.Content) int {
	return m.Count(contents)
}

// MockPruningPolicy is a mock implementation of ports.PruningPolicy.
type MockPruningPolicy struct {
	MarkTurnsFn func(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error)
	NameFn      func() string
}

func (m *MockPruningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	if m.MarkTurnsFn != nil {
		return m.MarkTurnsFn(ctx, turns, keep)
	}
	return 0, nil
}

func (m *MockPruningPolicy) Name() string {
	if m.NameFn != nil {
		return m.NameFn()
	}
	return "MockPolicy"
}
