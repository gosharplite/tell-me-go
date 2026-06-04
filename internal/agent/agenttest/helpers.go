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

// StubChatterComposer is a manual stub implementing ports.ChatterComposer.
// Use this when a test only needs to construct a Chatter (e.g., via
// factory.NewChatter). Set only the fields your test uses; leave the rest nil.
type StubChatterComposer struct {
	Gateway          llm.LLMGateway
	EventBus         events.EventBus
	Paths            *persistence.Paths
	HistoryManager   ports.HistoryManager
	Logger           ports.Logger
	Tracker          pricing.CostTracker
	PricingOverrides map[string]pricing.ModelPricing
	SessionProvider  ports.SessionProvider
	TurnsLogger      ports.TurnsLogger
	SecurityManager  security.Manager
	Registry         tools.Registry
	RegistryErr      error
}

var _ ports.ChatterComposer = (*StubChatterComposer)(nil)

func (s *StubChatterComposer) GetGateway() llm.LLMGateway              { return s.Gateway }
func (s *StubChatterComposer) GetEventBus() events.EventBus            { return s.EventBus }
func (s *StubChatterComposer) GetPaths() *persistence.Paths            { return s.Paths }
func (s *StubChatterComposer) GetHistoryManager() ports.HistoryManager { return s.HistoryManager }
func (s *StubChatterComposer) GetLogger() ports.Logger                 { return s.Logger }
func (s *StubChatterComposer) GetTracker() pricing.CostTracker         { return s.Tracker }
func (s *StubChatterComposer) GetPricingOverrides() map[string]pricing.ModelPricing {
	return s.PricingOverrides
}
func (s *StubChatterComposer) GetSessionProvider() ports.SessionProvider { return s.SessionProvider }
func (s *StubChatterComposer) GetTurnsLogger() ports.TurnsLogger         { return s.TurnsLogger }
func (s *StubChatterComposer) GetSecurityManager() security.Manager      { return s.SecurityManager }
func (s *StubChatterComposer) GetRegistry() (tools.Registry, error)      { return s.Registry, s.RegistryErr }

// StubSessionFinalizer is a manual stub implementing ports.SessionFinalizer.
// Use this when a test only needs to finalize a session (record costs).
type StubSessionFinalizer struct {
	Tracker          pricing.CostTracker
	Paths            *persistence.Paths
	PricingOverrides map[string]pricing.ModelPricing
}

var _ ports.SessionFinalizer = (*StubSessionFinalizer)(nil)

func (s *StubSessionFinalizer) GetTracker() pricing.CostTracker { return s.Tracker }
func (s *StubSessionFinalizer) GetPaths() *persistence.Paths    { return s.Paths }
func (s *StubSessionFinalizer) GetPricingOverrides() map[string]pricing.ModelPricing {
	return s.PricingOverrides
}

// StubEventBus is a stub implementation of events.EventBus for testing.
// Set ShutdownErr to control the error returned by Shutdown; all other
// methods are no-ops.
type StubEventBus struct {
	ShutdownErr error
}

var _ events.EventBus = (*StubEventBus)(nil)

func (s *StubEventBus) Publish(ctx context.Context, e events.Event) error { return nil }
func (s *StubEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (s *StubEventBus) Shutdown(ctx context.Context) error                { return s.ShutdownErr }
func (s *StubEventBus) Flush(ctx context.Context) error                   { return nil }
func (s *StubEventBus) Listen(ctx context.Context) error                  { <-ctx.Done(); return ctx.Err() }
func (s *StubEventBus) WaitStarted()                                      {}

// StubCapturer is a stub implementation of agent.CapturerInteractor for testing.
// Set fields to control return values; all other methods are no-ops.
type StubCapturer struct {
	IsTTYVal            bool
	CapturePromptResult string
	CapturePromptErr    error
	ConfirmResult       bool
	ConfirmErr          error
	CloseErr            error
}

var (
	_ ports.Capturer          = (*StubCapturer)(nil)
	_ security.UserInteractor = (*StubCapturer)(nil)
)

func (s *StubCapturer) IsTTY(v any) bool { return s.IsTTYVal }

func (s *StubCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	return s.CapturePromptResult, s.CapturePromptErr
}

func (s *StubCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	return s.ConfirmResult, s.ConfirmErr
}

func (s *StubCapturer) Close(ctx context.Context) error { return s.CloseErr }

func (s *StubCapturer) Warn(msg string)                                   {}
func (s *StubCapturer) Prompt(msg string)                                 {}
func (s *StubCapturer) ReadSingleKey(ctx context.Context) (string, error) { return "", nil }
func (s *StubCapturer) ReadLine(ctx context.Context) (string, error)      { return "", nil }

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
