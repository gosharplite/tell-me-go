// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	mu sync.Mutex

	// Func fields — when nil, the method returns zero values.
	IsPathSafeFunc       func(path string) (string, error)
	IsPathWritableFunc   func(path string) (string, error)
	AuthorizeFunc        func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
	LogAuditFunc         func(action string, args ...any)
	TerminalLockFunc     func()
	TerminalUnlockFunc   func()
	PromptFunc           func(message string)
	WarnFunc             func(message string)
	ConfirmFunc          func(ctx context.Context, message string) (bool, error)
	ReadLineFunc         func(ctx context.Context) (string, error)
	IsCommandAllowedFunc func(command string) bool
	IsBypassActiveFunc   func() bool
	CloseFunc            func() error

	// Call counters.
	calledIsPathSafe       int
	calledIsPathWritable   int
	calledAuthorize        int
	calledLogAudit         int
	calledTerminalLock     int
	calledTerminalUnlock   int
	calledPrompt           int
	calledWarn             int
	calledConfirm          int
	calledReadLine         int
	calledIsCommandAllowed int
	calledIsBypassActive   int
	calledClose            int

	// Last-arg capture fields for argument inspection in tests.
	lastIsPathSafe      string
	lastAuthorizeLabel  string
	lastAuthorizeDetail string
	lastAuthorizeReason string
	lastLogAudit        string
	lastPrompt          string
	lastWarn            string
	lastConfirmCtx      context.Context
	lastConfirmMessage  string
	lastCommand         string
}

// Snapshot returns a race-safe copy of all call counts.
func (m *mockServiceSecurityManager) Snapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]int{
		"IsPathSafe":       m.calledIsPathSafe,
		"IsPathWritable":   m.calledIsPathWritable,
		"Authorize":        m.calledAuthorize,
		"LogAudit":         m.calledLogAudit,
		"TerminalLock":     m.calledTerminalLock,
		"TerminalUnlock":   m.calledTerminalUnlock,
		"Prompt":           m.calledPrompt,
		"Warn":             m.calledWarn,
		"Confirm":          m.calledConfirm,
		"ReadLine":         m.calledReadLine,
		"IsCommandAllowed": m.calledIsCommandAllowed,
		"IsBypassActive":   m.calledIsBypassActive,
		"Close":            m.calledClose,
	}
}

func (m *mockServiceSecurityManager) IsPathSafe(path string) (string, error) {
	m.mu.Lock()
	m.calledIsPathSafe++
	m.lastIsPathSafe = path
	fn := m.IsPathSafeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(path)
	}
	return "", nil
}

func (m *mockServiceSecurityManager) IsPathWritable(path string) (string, error) {
	m.mu.Lock()
	m.calledIsPathWritable++
	fn := m.IsPathWritableFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(path)
	}
	return "", nil
}

func (m *mockServiceSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	m.mu.Lock()
	m.calledAuthorize++
	m.lastAuthorizeLabel = label
	m.lastAuthorizeDetail = detail
	m.lastAuthorizeReason = reason
	fn := m.AuthorizeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, label, detail, reason, isSafe)
	}
	return false, nil
}

func (m *mockServiceSecurityManager) LogAudit(action string, args ...any) {
	m.mu.Lock()
	m.calledLogAudit++
	m.lastLogAudit = action
	fn := m.LogAuditFunc
	m.mu.Unlock()
	if fn != nil {
		fn(action, args...)
	}
}

func (m *mockServiceSecurityManager) TerminalLock() {
	m.mu.Lock()
	m.calledTerminalLock++
	fn := m.TerminalLockFunc
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *mockServiceSecurityManager) TerminalUnlock() {
	m.mu.Lock()
	m.calledTerminalUnlock++
	fn := m.TerminalUnlockFunc
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (m *mockServiceSecurityManager) Prompt(message string) {
	m.mu.Lock()
	m.calledPrompt++
	m.lastPrompt = message
	fn := m.PromptFunc
	m.mu.Unlock()
	if fn != nil {
		fn(message)
	}
}

func (m *mockServiceSecurityManager) Warn(message string) {
	m.mu.Lock()
	m.calledWarn++
	m.lastWarn = message
	fn := m.WarnFunc
	m.mu.Unlock()
	if fn != nil {
		fn(message)
	}
}

func (m *mockServiceSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	m.mu.Lock()
	m.calledConfirm++
	m.lastConfirmCtx = ctx
	m.lastConfirmMessage = message
	fn := m.ConfirmFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, message)
	}
	return false, nil
}

func (m *mockServiceSecurityManager) ReadLine(ctx context.Context) (string, error) {
	m.mu.Lock()
	m.calledReadLine++
	fn := m.ReadLineFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return "", nil
}

func (m *mockServiceSecurityManager) IsCommandAllowed(command string) bool {
	m.mu.Lock()
	m.calledIsCommandAllowed++
	m.lastCommand = command
	fn := m.IsCommandAllowedFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(command)
	}
	return false
}

func (m *mockServiceSecurityManager) IsBypassActive() bool {
	m.mu.Lock()
	m.calledIsBypassActive++
	fn := m.IsBypassActiveFunc
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return false
}

func (m *mockServiceSecurityManager) Close() error {
	m.mu.Lock()
	m.calledClose++
	fn := m.CloseFunc
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return nil
}

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

// stubSessionFinalizer is a manual stub implementing ports.SessionFinalizer.
// Use this when a test only needs to finalize a session (record costs).
type stubSessionFinalizer struct {
	Tracker          pricing.CostTracker
	Paths            *persistence.Paths
	PricingOverrides map[string]pricing.ModelPricing
}

var _ ports.SessionFinalizer = (*stubSessionFinalizer)(nil)

func (s *stubSessionFinalizer) GetTracker() pricing.CostTracker { return s.Tracker }
func (s *stubSessionFinalizer) GetPaths() *persistence.Paths    { return s.Paths }
func (s *stubSessionFinalizer) GetPricingOverrides() map[string]pricing.ModelPricing {
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
	mu sync.Mutex

	// Func fields — when nil, the method returns zero values.
	ChatFunc      func(ctx context.Context, sess *ports.Session, prompt string) error
	SetLimitsFunc func(ctx context.Context, maxTurns, contextWindow, historyTurns int) error
	SubscribeFunc func(handler func(context.Context, events.Event))
	ShutdownFunc  func(ctx context.Context) error

	// Call counters.
	calledChat      int
	calledSetLimits int
	calledSubscribe int
	calledShutdown  int

	// Last-arg capture fields for argument inspection in tests.
	lastChatCtx          context.Context
	lastChatSess         *ports.Session
	lastChatPrompt       string
	lastSubscribeHandler func(context.Context, events.Event)
}

// Snapshot returns a race-safe copy of all call counts.
func (m *mockServiceAgent) Snapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]int{
		"Chat":      m.calledChat,
		"SetLimits": m.calledSetLimits,
		"Subscribe": m.calledSubscribe,
		"Shutdown":  m.calledShutdown,
	}
}

func (m *mockServiceAgent) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	m.mu.Lock()
	m.calledChat++
	m.lastChatCtx = ctx
	m.lastChatSess = sess
	m.lastChatPrompt = prompt
	fn := m.ChatFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, sess, prompt)
	}
	return nil
}

func (m *mockServiceAgent) SetLimits(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
	m.mu.Lock()
	m.calledSetLimits++
	fn := m.SetLimitsFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, maxTurns, contextWindow, historyTurns)
	}
	return nil
}

func (m *mockServiceAgent) Subscribe(handler func(context.Context, events.Event)) {
	m.mu.Lock()
	m.calledSubscribe++
	m.lastSubscribeHandler = handler
	fn := m.SubscribeFunc
	m.mu.Unlock()
	if fn != nil {
		fn(handler)
	}
}

func (m *mockServiceAgent) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.calledShutdown++
	fn := m.ShutdownFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

// MockServiceAgent is a mock of Chatter.
type MockServiceAgent = mockServiceAgent

type mockTurnsLogger struct {
	mu sync.Mutex

	// Func fields — when nil, the method returns zero values.
	HandleEventFunc func(ctx context.Context, e events.Event)
	ListenFunc      func(ctx context.Context) error
	CloseFunc       func() error

	// Call counters.
	calledHandleEvent int
	calledListen      int
	calledClose       int

	// Last-arg capture field for argument inspection in tests.
	lastHandleEvent events.Event
}

// Snapshot returns a race-safe copy of all call counts.
func (m *mockTurnsLogger) Snapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]int{
		"HandleEvent": m.calledHandleEvent,
		"Listen":      m.calledListen,
		"Close":       m.calledClose,
	}
}

func (m *mockTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {
	m.mu.Lock()
	m.calledHandleEvent++
	m.lastHandleEvent = e
	fn := m.HandleEventFunc
	m.mu.Unlock()
	if fn != nil {
		fn(ctx, e)
	}
}

func (m *mockTurnsLogger) Listen(ctx context.Context) error {
	m.mu.Lock()
	m.calledListen++
	fn := m.ListenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

func (m *mockTurnsLogger) Close() error {
	m.mu.Lock()
	m.calledClose++
	fn := m.CloseFunc
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return nil
}

// MockTurnsLogger is a mock of TurnsLogger.
type MockTurnsLogger = mockTurnsLogger
