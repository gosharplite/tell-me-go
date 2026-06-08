// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

// ---------------------------------------------------------------------------
// A. stubUIRenderer (13 methods)
// ---------------------------------------------------------------------------

func TestStubUIRenderer_StartSpinner(t *testing.T) {
	t.Parallel()

	s := &stubUIRenderer{}
	stop := s.StartSpinner(context.Background())
	if stop == nil {
		t.Fatal("StartSpinner returned nil func")
	}
	// Must not panic.
	stop()
}

func TestStubUIRenderer_StartSpinnerWithStatus(t *testing.T) {
	t.Parallel()

	s := &stubUIRenderer{}
	stop := s.StartSpinnerWithStatus(context.Background(), "loading")
	if stop == nil {
		t.Fatal("StartSpinnerWithStatus returned nil func")
	}
	stop()
}

func TestStubUIRenderer_StartSpinnerWithMetrics(t *testing.T) {
	t.Parallel()

	s := &stubUIRenderer{}
	stop := s.StartSpinnerWithMetrics(context.Background(), "processing")
	if stop == nil {
		t.Fatal("StartSpinnerWithMetrics returned nil func")
	}
	stop()
}

func TestStubUIRenderer_NoOpMethods(t *testing.T) {
	t.Parallel()

	s := &stubUIRenderer{}
	ctx := context.Background()

	// All of these must not panic.
	s.RenderResponse(ctx, nil, false, false)
	s.RenderResponse(ctx, &llm.Content{Role: "assistant"}, true, true)

	s.LogTurnStatus(ctx, events.TurnStatus{CurrentTurns: 1})
	s.LogTurnStatus(ctx, events.TurnStatus{})

	s.LogSystemMessage(ctx, "hello", "info")
	s.LogSystemMessage(ctx, "", "")

	s.LogUsage(ctx, &llm.Metrics{}, "log.json", time.Now())
	s.LogUsage(ctx, nil, "", time.Time{})

	s.LogToolCall(ctx, nil, 0, 0, false)
	s.LogToolCall(ctx, []*llm.FunctionCall{{Name: "test"}}, 1, 10, true)

	s.LogToolResult(ctx, "tool", tools.ToolResult{Text: "ok"}, false)
	s.LogToolResult(ctx, "", tools.ToolResult{}, true)

	s.RenderHealthReport(ctx, nil)
	s.RenderHealthReport(ctx, &ports.HealthReport{})

	s.SetUseColor(true)
	s.SetUseColor(false)

	s.SetForceSpinner(true)
	s.SetForceSpinner(false)
}

func TestStubUIRenderer_IsTerminalContext(t *testing.T) {
	t.Parallel()

	s := &stubUIRenderer{}
	if s.IsTerminalContext() {
		t.Error("stubUIRenderer.IsTerminalContext should return false")
	}
}

// ---------------------------------------------------------------------------
// B. stubHistoryRenderer (1 method)
// ---------------------------------------------------------------------------

func TestStubHistoryRenderer_Render(t *testing.T) {
	t.Parallel()

	s := &stubHistoryRenderer{}
	// Must not panic with nil/zero args.
	s.Render(nil, nil, 0, ports.HistoryRenderOptions{})
	s.Render(io.Discard, nil, 5, ports.HistoryRenderOptions{})
}

// ---------------------------------------------------------------------------
// C. stubHistoryBrowser (1 method)
// ---------------------------------------------------------------------------

func TestStubHistoryBrowser_Browse(t *testing.T) {
	t.Parallel()

	s := &stubHistoryBrowser{}
	err := s.Browse(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("Browse should return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// D. StubChatterComposer (11 getters + RegistryErr)
// ---------------------------------------------------------------------------

// newPopulatedStubChatterComposer returns a StubChatterComposer with all fields
// set to non-nil mock values, used by TestStubChatterComposer_Getters_* tests.
func newPopulatedStubChatterComposer() *StubChatterComposer {
	return &StubChatterComposer{
		Gateway:          new(MockGateway),
		EventBus:         &StubEventBus{},
		Paths:            &persistence.Paths{ModeDir: "/tmp"},
		HistoryManager:   new(MockHistoryManager),
		Logger:           &ports.NoOpLogger{},
		Tracker:          new(MockCostTracker),
		PricingOverrides: map[string]pricing.ModelPricing{"gpt-4": {Hit: 0.03, Miss: 0.06}},
		SessionProvider:  new(MockSessionProvider),
		TurnsLogger:      &ports.NoOpTurnsLogger{},
		SecurityManager:  new(MockServiceSecurityManager),
		Registry:         NewMockToolRegistry(),
	}
}

func TestStubChatterComposer_Getters_Gateway(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetGateway(); got != c.Gateway {
		t.Errorf("GetGateway mismatch: got %v, want %v", got, c.Gateway)
	}
}

func TestStubChatterComposer_Getters_EventBus(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetEventBus(); got != c.EventBus {
		t.Errorf("GetEventBus mismatch: got %v, want %v", got, c.EventBus)
	}
}

func TestStubChatterComposer_Getters_Paths(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetPaths(); got != c.Paths {
		t.Errorf("GetPaths mismatch: got %v, want %v", got, c.Paths)
	}
}

func TestStubChatterComposer_Getters_HistoryManager(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetHistoryManager(); got != c.HistoryManager {
		t.Errorf("GetHistoryManager mismatch: got %v, want %v", got, c.HistoryManager)
	}
}

func TestStubChatterComposer_Getters_Logger(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetLogger(); got != c.Logger {
		t.Errorf("GetLogger mismatch: got %v, want %v", got, c.Logger)
	}
}

func TestStubChatterComposer_Getters_Tracker(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetTracker(); got != c.Tracker {
		t.Errorf("GetTracker mismatch: got %v, want %v", got, c.Tracker)
	}
}

func TestStubChatterComposer_Getters_PricingOverrides(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetPricingOverrides(); got == nil {
		t.Error("GetPricingOverrides should not be nil")
	}
}

func TestStubChatterComposer_Getters_SessionProvider(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetSessionProvider(); got != c.SessionProvider {
		t.Errorf("GetSessionProvider mismatch: got %v, want %v", got, c.SessionProvider)
	}
}

func TestStubChatterComposer_Getters_TurnsLogger(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetTurnsLogger(); got != c.TurnsLogger {
		t.Errorf("GetTurnsLogger mismatch: got %v, want %v", got, c.TurnsLogger)
	}
}

func TestStubChatterComposer_Getters_SecurityManager(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	if got := c.GetSecurityManager(); got != c.SecurityManager {
		t.Errorf("GetSecurityManager mismatch: got %v, want %v", got, c.SecurityManager)
	}
}

func TestStubChatterComposer_Getters_Registry(t *testing.T) {
	t.Parallel()
	c := newPopulatedStubChatterComposer()
	gotReg, gotErr := c.GetRegistry()
	if gotReg != c.Registry {
		t.Errorf("GetRegistry mismatch: got %v, want %v", gotReg, c.Registry)
	}
	if gotErr != nil {
		t.Errorf("GetRegistry unexpected error: %v", gotErr)
	}
}

func TestStubChatterComposer_RegistryErr(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	c := &StubChatterComposer{RegistryErr: boom}

	gotReg, gotErr := c.GetRegistry()
	if gotReg != nil {
		t.Errorf("GetRegistry should return nil when error is set, got %v", gotReg)
	}
	if gotErr != boom {
		t.Errorf("GetRegistry error = %v, want %v", gotErr, boom)
	}
}

func TestStubChatterComposer_NilFields_Gateway(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetGateway() != nil {
		t.Error("nil Gateway should return nil")
	}
}

func TestStubChatterComposer_NilFields_EventBus(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetEventBus() != nil {
		t.Error("nil EventBus should return nil")
	}
}

func TestStubChatterComposer_NilFields_Paths(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetPaths() != nil {
		t.Error("nil Paths should return nil")
	}
}

func TestStubChatterComposer_NilFields_HistoryManager(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetHistoryManager() != nil {
		t.Error("nil HistoryManager should return nil")
	}
}

func TestStubChatterComposer_NilFields_Logger(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetLogger() != nil {
		t.Error("nil Logger should return nil")
	}
}

func TestStubChatterComposer_NilFields_Tracker(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetTracker() != nil {
		t.Error("nil Tracker should return nil")
	}
}

func TestStubChatterComposer_NilFields_PricingOverrides(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	overrides := c.GetPricingOverrides()
	if overrides != nil {
		t.Errorf("nil PricingOverrides should return nil, got %v", overrides)
	}
}

func TestStubChatterComposer_NilFields_SessionProvider(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetSessionProvider() != nil {
		t.Error("nil SessionProvider should return nil")
	}
}

func TestStubChatterComposer_NilFields_TurnsLogger(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetTurnsLogger() != nil {
		t.Error("nil TurnsLogger should return nil")
	}
}

func TestStubChatterComposer_NilFields_SecurityManager(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	if c.GetSecurityManager() != nil {
		t.Error("nil SecurityManager should return nil")
	}
}

func TestStubChatterComposer_NilFields_Registry(t *testing.T) {
	t.Parallel()
	c := &StubChatterComposer{}
	gotReg, gotErr := c.GetRegistry()
	if gotReg != nil {
		t.Errorf("nil Registry should return nil, got %v", gotReg)
	}
	if gotErr != nil {
		t.Errorf("nil RegistryErr should return nil error, got %v", gotErr)
	}
}

// ---------------------------------------------------------------------------
// E. StubSessionFinalizer (3 getters)
// ---------------------------------------------------------------------------

func TestStubSessionFinalizer_Getters(t *testing.T) {
	t.Parallel()

	tracker := new(MockCostTracker)
	p := &persistence.Paths{ModeDir: "/app"}
	overrides := map[string]pricing.ModelPricing{
		"claude": {Hit: 0.01, Miss: 0.05},
	}

	s := &StubSessionFinalizer{
		Tracker:          tracker,
		Paths:            p,
		PricingOverrides: overrides,
	}

	if s.GetTracker() != tracker {
		t.Error("GetTracker mismatch")
	}
	if s.GetPaths() != p {
		t.Error("GetPaths mismatch")
	}
	if s.GetPricingOverrides() == nil {
		t.Error("GetPricingOverrides should not be nil")
	}
}

func TestStubSessionFinalizer_NilFields(t *testing.T) {
	t.Parallel()

	s := &StubSessionFinalizer{}

	if s.GetTracker() != nil {
		t.Error("nil Tracker should return nil")
	}
	if s.GetPaths() != nil {
		t.Error("nil Paths should return nil")
	}
	if s.GetPricingOverrides() != nil {
		t.Error("nil PricingOverrides should return nil")
	}
}

// ---------------------------------------------------------------------------
// F. StubEventBus (6 methods)
// ---------------------------------------------------------------------------

func TestStubEventBus_Publish(t *testing.T) {
	t.Parallel()

	bus := &StubEventBus{}
	err := bus.Publish(context.Background(), events.TurnStatusEvent{})
	if err != nil {
		t.Errorf("Publish should return nil, got %v", err)
	}
}

func TestStubEventBus_Subscribe(t *testing.T) {
	t.Parallel()

	bus := &StubEventBus{}
	// Must not panic.
	bus.Subscribe(func(ctx context.Context, e events.Event) {})
}

func TestStubEventBus_Shutdown_NilErr(t *testing.T) {
	t.Parallel()

	bus := &StubEventBus{}
	err := bus.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown with nil ShutdownErr should return nil, got %v", err)
	}
}

func TestStubEventBus_Shutdown_WithErr(t *testing.T) {
	t.Parallel()

	want := errors.New("shutdown failed")
	bus := &StubEventBus{ShutdownErr: want}
	err := bus.Shutdown(context.Background())
	if err != want {
		t.Errorf("Shutdown error = %v, want %v", err, want)
	}
}

func TestStubEventBus_Flush(t *testing.T) {
	t.Parallel()

	bus := &StubEventBus{}
	err := bus.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush should return nil, got %v", err)
	}
}

func TestStubEventBus_Listen(t *testing.T) {
	t.Parallel()

	bus := &StubEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bus.Listen(ctx)
	if err != context.Canceled {
		t.Errorf("Listen after cancel should return context.Canceled, got %v", err)
	}
}

func TestStubEventBus_WaitStarted(t *testing.T) {
	t.Parallel()

	bus := &StubEventBus{}
	// Must not panic.
	bus.WaitStarted()
}

// ---------------------------------------------------------------------------
// G. StubCapturer (9 methods)
// ---------------------------------------------------------------------------

func TestStubCapturer_IsTTY_True(t *testing.T) {
	t.Parallel()

	c := &StubCapturer{IsTTYVal: true}
	if !c.IsTTY(nil) {
		t.Error("IsTTY should return true when IsTTYVal is true")
	}
}

func TestStubCapturer_IsTTY_False(t *testing.T) {
	t.Parallel()

	c := &StubCapturer{IsTTYVal: false}
	if c.IsTTY(nil) {
		t.Error("IsTTY should return false when IsTTYVal is false")
	}
}

func TestStubCapturer_CapturePrompt(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{
			CapturePromptResult: "user input",
			CapturePromptErr:    nil,
		}
		got, err := c.CapturePrompt(context.Background(), nil)
		if got != "user input" {
			t.Errorf("CapturePrompt = %q, want %q", got, "user input")
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		want := errors.New("capture failed")
		c := &StubCapturer{
			CapturePromptResult: "",
			CapturePromptErr:    want,
		}
		got, err := c.CapturePrompt(context.Background(), nil)
		if got != "" {
			t.Errorf("CapturePrompt = %q, want empty", got)
		}
		if err != want {
			t.Errorf("CapturePrompt error = %v, want %v", err, want)
		}
	})
}

func TestStubCapturer_Confirm(t *testing.T) {
	t.Parallel()

	t.Run("true", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{ConfirmResult: true}
		got, err := c.Confirm(context.Background(), "proceed?")
		if !got {
			t.Error("Confirm should return true")
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("false", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{ConfirmResult: false}
		got, err := c.Confirm(context.Background(), "proceed?")
		if got {
			t.Error("Confirm should return false")
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		want := errors.New("confirm failed")
		c := &StubCapturer{ConfirmErr: want}
		got, err := c.Confirm(context.Background(), "proceed?")
		if got {
			t.Error("Confirm should return false on error")
		}
		if err != want {
			t.Errorf("Confirm error = %v, want %v", err, want)
		}
	})
}

func TestStubCapturer_Close(t *testing.T) {
	t.Parallel()

	t.Run("no error", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{}
		if err := c.Close(context.Background()); err != nil {
			t.Errorf("Close should return nil, got %v", err)
		}
	})

	t.Run("with error", func(t *testing.T) {
		t.Parallel()
		want := errors.New("close failed")
		c := &StubCapturer{CloseErr: want}
		if err := c.Close(context.Background()); err != want {
			t.Errorf("Close error = %v, want %v", err, want)
		}
	})
}

func TestStubCapturer_NoOpMethods(t *testing.T) {
	t.Parallel()

	c := &StubCapturer{}

	// Must not panic.
	c.Warn("warning message")
	c.Prompt("prompt message")

	got, err := c.ReadSingleKey(context.Background())
	if got != "" {
		t.Errorf("ReadSingleKey should return empty, got %q", got)
	}
	if err != nil {
		t.Errorf("ReadSingleKey unexpected error: %v", err)
	}

	got, err = c.ReadLine(context.Background())
	if got != "" {
		t.Errorf("ReadLine should return empty, got %q", got)
	}
	if err != nil {
		t.Errorf("ReadLine unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// H. mockServiceSecurityManager (13 testify mock methods)
// ---------------------------------------------------------------------------

func TestMockServiceSecurityManager_IsPathSafe(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("IsPathSafe", "/safe/path").Return("/safe/path", nil)

	got, err := m.IsPathSafe("/safe/path")
	if got != "/safe/path" {
		t.Errorf("IsPathSafe = %q, want %q", got, "/safe/path")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_IsPathWritable(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("IsPathWritable", "/tmp").Return("/tmp", nil)

	got, err := m.IsPathWritable("/tmp")
	if got != "/tmp" {
		t.Errorf("IsPathWritable = %q, want %q", got, "/tmp")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_Authorize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockServiceSecurityManager)
	m.On("Authorize", ctx, "label", "detail", "reason", true).Return(true, nil)

	got, err := m.Authorize(ctx, "label", "detail", "reason", true)
	if !got {
		t.Error("Authorize should return true")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_Authorize_Denied(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockServiceSecurityManager)
	m.On("Authorize", ctx, "label", "detail", "reason", false).Return(false, nil)

	got, err := m.Authorize(ctx, "label", "detail", "reason", false)
	if got {
		t.Error("Authorize should return false")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_Authorize_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("auth failed")
	m := new(MockServiceSecurityManager)
	m.On("Authorize", ctx, "label", "detail", "reason", false).Return(false, want)

	got, err := m.Authorize(ctx, "label", "detail", "reason", false)
	if got {
		t.Error("Authorize should return false on error")
	}
	if err != want {
		t.Errorf("Authorize error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_LogAudit(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("LogAudit", "action", mock.Anything).Return()

	// Must not panic.
	m.LogAudit("action", "arg1", "arg2")
	m.AssertCalled(t, "LogAudit", "action", mock.Anything)
}

func TestMockServiceSecurityManager_TerminalLock(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("TerminalLock").Return()

	m.TerminalLock()
	m.AssertCalled(t, "TerminalLock")
}

func TestMockServiceSecurityManager_TerminalUnlock(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("TerminalUnlock").Return()

	m.TerminalUnlock()
	m.AssertCalled(t, "TerminalUnlock")
}

func TestMockServiceSecurityManager_Prompt(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("Prompt", "message").Return()

	m.Prompt("message")
	m.AssertCalled(t, "Prompt", "message")
}

func TestMockServiceSecurityManager_Warn(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("Warn", "warning").Return()

	m.Warn("warning")
	m.AssertCalled(t, "Warn", "warning")
}

func TestMockServiceSecurityManager_Confirm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockServiceSecurityManager)
	m.On("Confirm", ctx, "proceed?").Return(true, nil)

	got, err := m.Confirm(ctx, "proceed?")
	if !got {
		t.Error("Confirm should return true")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_ReadLine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockServiceSecurityManager)
	m.On("ReadLine", ctx).Return("input", nil)

	got, err := m.ReadLine(ctx)
	if got != "input" {
		t.Errorf("ReadLine = %q, want %q", got, "input")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_IsCommandAllowed(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("IsCommandAllowed", "ls").Return(true)

	if !m.IsCommandAllowed("ls") {
		t.Error("IsCommandAllowed should return true")
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_IsBypassActive(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("IsBypassActive").Return(false)

	if m.IsBypassActive() {
		t.Error("IsBypassActive should return false")
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_Close(t *testing.T) {
	t.Parallel()

	m := new(MockServiceSecurityManager)
	m.On("Close").Return(nil)

	if err := m.Close(); err != nil {
		t.Errorf("Close unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceSecurityManager_Close_Error(t *testing.T) {
	t.Parallel()

	want := errors.New("close failed")
	m := new(MockServiceSecurityManager)
	m.On("Close").Return(want)

	if err := m.Close(); err != want {
		t.Errorf("Close error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// I. mockServiceAgent (4 testify mock methods)
// ---------------------------------------------------------------------------

func TestMockServiceAgent_Chat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}
	m := new(MockServiceAgent)
	m.On("Chat", ctx, sess, "hello").Return(nil)

	err := m.Chat(ctx, sess, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceAgent_Chat_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}
	want := errors.New("chat failed")
	m := new(MockServiceAgent)
	m.On("Chat", ctx, sess, "hello").Return(want)

	err := m.Chat(ctx, sess, "hello")
	if err != want {
		t.Errorf("Chat error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}

func TestMockServiceAgent_SetLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockServiceAgent)
	m.On("SetLimits", ctx, 5, 1000, 10).Return(nil)

	err := m.SetLimits(ctx, 5, 1000, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceAgent_SetLimits_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("limits invalid")
	m := new(MockServiceAgent)
	m.On("SetLimits", ctx, -1, 1000, 10).Return(want)

	err := m.SetLimits(ctx, -1, 1000, 10)
	if err != want {
		t.Errorf("SetLimits error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}

func TestMockServiceAgent_Subscribe(t *testing.T) {
	t.Parallel()

	sub := func(ctx context.Context, ev events.Event) {}
	m := new(MockServiceAgent)
	m.On("Subscribe", mock.Anything).Return()

	m.Subscribe(sub)
	m.AssertCalled(t, "Subscribe", mock.Anything)
}

func TestMockServiceAgent_Shutdown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockServiceAgent)
	m.On("Shutdown", ctx).Return(nil)

	err := m.Shutdown(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockServiceAgent_Shutdown_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("shutdown failed")
	m := new(MockServiceAgent)
	m.On("Shutdown", ctx).Return(want)

	err := m.Shutdown(ctx)
	if err != want {
		t.Errorf("Shutdown error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// J. mockTurnsLogger (3 testify mock methods)
// ---------------------------------------------------------------------------

func TestMockTurnsLogger_HandleEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ev := events.TurnStatusEvent{}
	m := new(MockTurnsLogger)
	m.On("HandleEvent", ctx, ev).Return()

	m.HandleEvent(ctx, ev)
	m.AssertCalled(t, "HandleEvent", ctx, ev)
}

func TestMockTurnsLogger_Listen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockTurnsLogger)
	m.On("Listen", ctx).Return(nil)

	err := m.Listen(ctx)
	if err != nil {
		t.Errorf("Listen unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockTurnsLogger_Listen_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("listen failed")
	m := new(MockTurnsLogger)
	m.On("Listen", ctx).Return(want)

	err := m.Listen(ctx)
	if err != want {
		t.Errorf("Listen error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}

func TestMockTurnsLogger_Close(t *testing.T) {
	t.Parallel()

	m := new(MockTurnsLogger)
	m.On("Close").Return(nil)

	if err := m.Close(); err != nil {
		t.Errorf("Close unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockTurnsLogger_Close_Error(t *testing.T) {
	t.Parallel()

	want := errors.New("close failed")
	m := new(MockTurnsLogger)
	m.On("Close").Return(want)

	if err := m.Close(); err != want {
		t.Errorf("Close error = %v, want %v", err, want)
	}
	m.AssertExpectations(t)
}
