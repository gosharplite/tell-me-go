// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

// stubUIRenderer is a stub implementation of ports.UIRenderer for testing.
type stubUIRenderer struct{}

func (s *stubUIRenderer) StartSpinner(ctx context.Context) func() { return func() {} }
func (s *stubUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	return func() {}
}
func (s *stubUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	return func() {}
}
func (s *stubUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
}
func (s *stubUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus)    {}
func (s *stubUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {}
func (s *stubUIRenderer) LogUsage(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time) {
}
func (s *stubUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
}
func (s *stubUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
}
func (s *stubUIRenderer) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {}
func (s *stubUIRenderer) SetUseColor(use bool)                                               {}
func (s *stubUIRenderer) SetForceSpinner(force bool)                                         {}
func (s *stubUIRenderer) IsTerminalContext() bool                                            { return false }

// StubUIRenderer is a stub implementation of ports.UIRenderer for testing.
type StubUIRenderer = stubUIRenderer

// stubHistoryRenderer is a stub implementation of ports.HistoryRenderer for testing.
type stubHistoryRenderer struct{}

func (s *stubHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
}

// StubHistoryRenderer is a stub implementation of ports.HistoryRenderer for testing.
type StubHistoryRenderer = stubHistoryRenderer

// stubHistoryBrowser is a stub implementation of ports.HistoryBrowser for testing.
type stubHistoryBrowser struct{}

func (s *stubHistoryBrowser) Browse(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	return nil
}

// StubHistoryBrowser is a stub implementation of ports.HistoryBrowser for testing.
type StubHistoryBrowser = stubHistoryBrowser

// mockServiceSecurityManager is a mock of Manager.
type mockServiceSecurityManager struct {
	mock.Mock
}

func (m *mockServiceSecurityManager) IsPathSafe(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) IsPathWritable(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	args := m.Called(ctx, label, detail, reason, isSafe)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceSecurityManager) LogAudit(action string, args ...any) {
	m.Called(action, args)
}
func (m *mockServiceSecurityManager) TerminalLock()         { m.Called() }
func (m *mockServiceSecurityManager) TerminalUnlock()       { m.Called() }
func (m *mockServiceSecurityManager) Prompt(message string) { m.Called(message) }
func (m *mockServiceSecurityManager) Warn(message string)   { m.Called(message) }
func (m *mockServiceSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceSecurityManager) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) IsCommandAllowed(command string) bool {
	return m.Called(command).Bool(0)
}
func (m *mockServiceSecurityManager) IsBypassActive() bool { return m.Called().Bool(0) }
func (m *mockServiceSecurityManager) Close() error         { return m.Called().Error(0) }

// MockServiceSecurityManager is a mock of Manager.
type MockServiceSecurityManager = mockServiceSecurityManager

// mockServiceSessionDependencies is a mock of SessionDependencies.
type mockServiceSessionDependencies struct {
	mock.Mock
}

func (m *mockServiceSessionDependencies) GetGateway() llm.LLMGateway { return nil }
func (m *mockServiceSessionDependencies) GetHistoryManager() ports.HistoryManager {
	return m.Called().Get(0).(ports.HistoryManager)
}
func (m *mockServiceSessionDependencies) GetRegistry() (tools.Registry, error) { return nil, nil }
func (m *mockServiceSessionDependencies) GetSecurityManager() security.Manager {
	return m.Called().Get(0).(security.Manager)
}
func (m *mockServiceSessionDependencies) GetEventBus() events.EventBus {
	return m.Called().Get(0).(events.EventBus)
}
func (m *mockServiceSessionDependencies) GetLogger() ports.Logger {
	return m.Called().Get(0).(ports.Logger)
}
func (m *mockServiceSessionDependencies) GetTurnsLogger() ports.TurnsLogger {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.TurnsLogger)
}
func (m *mockServiceSessionDependencies) GetPaths() *persistence.Paths {
	return m.Called().Get(0).(*persistence.Paths)
}
func (m *mockServiceSessionDependencies) GetPricingOverrides() map[string]pricing.ModelPricing {
	return nil
}
func (m *mockServiceSessionDependencies) GetTracker() pricing.CostTracker { return nil }
func (m *mockServiceSessionDependencies) GetPricingData() pricing.PricingData {
	return pricing.PricingData{}
}
func (m *mockServiceSessionDependencies) GetClient() llm.LLMClient { return nil }
func (m *mockServiceSessionDependencies) GetSessionProvider() ports.SessionProvider {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.SessionProvider)
}
func (m *mockServiceSessionDependencies) GetHealthManager() ports.HealthCheckManager {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.HealthCheckManager)
}

// MockServiceSessionDependencies is a mock of SessionDependencies.
type MockServiceSessionDependencies = mockServiceSessionDependencies

// mockServiceEventBus is a mock of EventBus.
type mockServiceEventBus struct {
	mock.Mock
}

func (m *mockServiceEventBus) Publish(ctx context.Context, e events.Event) error {
	return m.Called(ctx, e).Error(0)
}
func (m *mockServiceEventBus) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}
func (m *mockServiceEventBus) Shutdown(ctx context.Context) error { return m.Called(ctx).Error(0) }
func (m *mockServiceEventBus) Flush(ctx context.Context) error    { return m.Called(ctx).Error(0) }
func (m *mockServiceEventBus) Listen(ctx context.Context) error   { <-ctx.Done(); return ctx.Err() }
func (m *mockServiceEventBus) WaitStarted()                       { m.Called() }

// MockServiceEventBus is a mock of EventBus.
type MockServiceEventBus = mockServiceEventBus

// mockServiceAgent is a mock of Chatter.
type mockServiceAgent struct {
	mock.Mock
}

func (m *mockServiceAgent) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return m.Called(ctx, sess, prompt).Error(0)
}
func (m *mockServiceAgent) SetLimits(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
	return m.Called(ctx, maxTurns, contextWindow, historyTurns).Error(0)
}
func (m *mockServiceAgent) Subscribe(handler func(context.Context, events.Event)) { m.Called(handler) }
func (m *mockServiceAgent) Shutdown(ctx context.Context) error                    { return m.Called(ctx).Error(0) }

// MockServiceAgent is a mock of Chatter.
type MockServiceAgent = mockServiceAgent

// mockServiceCapturer is a mock of Capturer.
type mockServiceCapturer struct {
	mock.Mock
}

func (m *mockServiceCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *mockServiceCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	callArgs := m.Called(ctx, args, opts)
	return callArgs.String(0), callArgs.Error(1)
}

func (m *mockServiceCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceCapturer) Warn(msg string)   { m.Called(msg) }
func (m *mockServiceCapturer) Prompt(msg string) { m.Called(msg) }
func (m *mockServiceCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceCapturer) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockServiceCapturer is a mock of Capturer.
type MockServiceCapturer = mockServiceCapturer

type mockTurnsLogger struct {
	mock.Mock
}

func (m *mockTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {
	m.Called(ctx, e)
}

func (m *mockTurnsLogger) Listen(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockTurnsLogger) Close() error {
	return m.Called().Error(0)
}

// MockTurnsLogger is a mock of TurnsLogger.
type MockTurnsLogger = mockTurnsLogger
