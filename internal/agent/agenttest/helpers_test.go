// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
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

	ctx := context.Background()

	tests := []struct {
		name string
		fn   func()
	}{
		// RenderResponse
		{"RenderResponse_normal", func() {
			s := &stubUIRenderer{}
			s.RenderResponse(ctx, &llm.Content{Role: "assistant"}, true, true)
		}},
		{"RenderResponse_nil_content", func() {
			s := &stubUIRenderer{}
			s.RenderResponse(ctx, nil, false, false)
		}},
		// LogTurnStatus
		{"LogTurnStatus_with_turns", func() {
			s := &stubUIRenderer{}
			s.LogTurnStatus(ctx, events.TurnStatus{CurrentTurns: 1})
		}},
		{"LogTurnStatus_empty", func() {
			s := &stubUIRenderer{}
			s.LogTurnStatus(ctx, events.TurnStatus{})
		}},
		// LogSystemMessage
		{"LogSystemMessage_normal", func() {
			s := &stubUIRenderer{}
			s.LogSystemMessage(ctx, "hello", "info")
		}},
		{"LogSystemMessage_empty", func() {
			s := &stubUIRenderer{}
			s.LogSystemMessage(ctx, "", "")
		}},
		// LogUsage
		{"LogUsage_with_metrics", func() {
			s := &stubUIRenderer{}
			s.LogUsage(ctx, &llm.Metrics{}, "log.json", time.Now())
		}},
		{"LogUsage_nil_metrics", func() {
			s := &stubUIRenderer{}
			s.LogUsage(ctx, nil, "", time.Time{})
		}},
		// LogToolCall
		{"LogToolCall_with_calls", func() {
			s := &stubUIRenderer{}
			s.LogToolCall(ctx, []*llm.FunctionCall{{Name: "test"}}, 1, 10, true)
		}},
		{"LogToolCall_nil_calls", func() {
			s := &stubUIRenderer{}
			s.LogToolCall(ctx, nil, 0, 0, false)
		}},
		// LogToolResult
		{"LogToolResult_with_result", func() {
			s := &stubUIRenderer{}
			s.LogToolResult(ctx, "tool", tools.ToolResult{Text: "ok"}, true)
		}},
		{"LogToolResult_empty", func() {
			s := &stubUIRenderer{}
			s.LogToolResult(ctx, "", tools.ToolResult{}, false)
		}},
		// RenderHealthReport
		{"RenderHealthReport_with_report", func() {
			s := &stubUIRenderer{}
			s.RenderHealthReport(ctx, &ports.HealthReport{})
		}},
		{"RenderHealthReport_nil_report", func() {
			s := &stubUIRenderer{}
			s.RenderHealthReport(ctx, nil)
		}},
		// SetUseColor
		{"SetUseColor_true", func() {
			s := &stubUIRenderer{}
			s.SetUseColor(true)
		}},
		{"SetUseColor_false", func() {
			s := &stubUIRenderer{}
			s.SetUseColor(false)
		}},
		// SetForceSpinner
		{"SetForceSpinner_true", func() {
			s := &stubUIRenderer{}
			s.SetForceSpinner(true)
		}},
		{"SetForceSpinner_false", func() {
			s := &stubUIRenderer{}
			s.SetForceSpinner(false)
		}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, tt.fn, "stubUIRenderer no-op method must not panic")
		})
	}
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

	t.Run("nil_writer", func(t *testing.T) {
		t.Parallel()
		s := &stubHistoryRenderer{}
		s.Render(nil, nil, 0, ports.HistoryRenderOptions{})
	})

	t.Run("valid_writer", func(t *testing.T) {
		t.Parallel()
		s := &stubHistoryRenderer{}
		s.Render(io.Discard, nil, 5, ports.HistoryRenderOptions{})
	})
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
		SessionProvider:  new(testfixtures.MockSessionProvider),
		TurnsLogger:      &ports.NoOpTurnsLogger{},
		SecurityManager:  &MockServiceSecurityManager{},
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
// E. stubSessionFinalizer (3 getters)
// ---------------------------------------------------------------------------

func Test_stubSessionFinalizer_Getters(t *testing.T) {
	t.Parallel()

	tracker := new(MockCostTracker)
	p := &persistence.Paths{ModeDir: "/app"}
	overrides := map[string]pricing.ModelPricing{
		"claude": {Hit: 0.01, Miss: 0.05},
	}

	s := &stubSessionFinalizer{
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

func Test_stubSessionFinalizer_NilFields(t *testing.T) {
	t.Parallel()

	s := &stubSessionFinalizer{}

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

	t.Run("noop", func(t *testing.T) {
		t.Parallel()
		bus := &StubEventBus{}
		bus.Subscribe(func(ctx context.Context, e events.Event) {})
	})
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

	t.Run("noop", func(t *testing.T) {
		t.Parallel()
		bus := &StubEventBus{}
		bus.WaitStarted()
	})
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

	t.Run("Warn", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{}
		c.Warn("warning message")
	})

	t.Run("Prompt", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{}
		c.Prompt("prompt message")
	})

	t.Run("ReadSingleKey", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{}
		got, err := c.ReadSingleKey(context.Background())
		if got != "" {
			t.Errorf("ReadSingleKey should return empty, got %q", got)
		}
		if err != nil {
			t.Errorf("ReadSingleKey unexpected error: %v", err)
		}
	})

	t.Run("ReadLine", func(t *testing.T) {
		t.Parallel()
		c := &StubCapturer{}
		got, err := c.ReadLine(context.Background())
		if got != "" {
			t.Errorf("ReadLine should return empty, got %q", got)
		}
		if err != nil {
			t.Errorf("ReadLine unexpected error: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// H. mockServiceSecurityManager (13 hand-rolled spy methods)
// ---------------------------------------------------------------------------

func TestMockServiceSecurityManager_IsPathSafe(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		IsPathSafeFunc: func(path string) (string, error) {
			return path, nil
		},
	}
	got, err := m.IsPathSafe("/safe/path")
	if got != "/safe/path" {
		t.Errorf("IsPathSafe = %q, want %q", got, "/safe/path")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["IsPathSafe"] != 1 {
		t.Errorf("expected 1 IsPathSafe call, got %d", snap["IsPathSafe"])
	}
}

func TestMockServiceSecurityManager_IsPathWritable(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		IsPathWritableFunc: func(path string) (string, error) {
			return path, nil
		},
	}
	got, err := m.IsPathWritable("/tmp")
	if got != "/tmp" {
		t.Errorf("IsPathWritable = %q, want %q", got, "/tmp")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["IsPathWritable"] != 1 {
		t.Errorf("expected 1 IsPathWritable call, got %d", snap["IsPathWritable"])
	}
}

func TestMockServiceSecurityManager_Authorize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockServiceSecurityManager{
		AuthorizeFunc: func(_ context.Context, _, _, _ string, _ bool) (bool, error) {
			return true, nil
		},
	}
	got, err := m.Authorize(ctx, "label", "detail", "reason", true)
	if !got {
		t.Error("Authorize should return true")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["Authorize"] != 1 {
		t.Errorf("expected 1 Authorize call, got %d", snap["Authorize"])
	}
}

func TestMockServiceSecurityManager_Authorize_Denied(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockServiceSecurityManager{
		AuthorizeFunc: func(_ context.Context, _, _, _ string, _ bool) (bool, error) {
			return false, nil
		},
	}
	got, err := m.Authorize(ctx, "label", "detail", "reason", false)
	if got {
		t.Error("Authorize should return false")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["Authorize"] != 1 {
		t.Errorf("expected 1 Authorize call, got %d", snap["Authorize"])
	}
}

func TestMockServiceSecurityManager_Authorize_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("auth failed")
	m := &MockServiceSecurityManager{
		AuthorizeFunc: func(_ context.Context, _, _, _ string, _ bool) (bool, error) {
			return false, want
		},
	}
	got, err := m.Authorize(ctx, "label", "detail", "reason", false)
	if got {
		t.Error("Authorize should return false on error")
	}
	if err != want {
		t.Errorf("Authorize error = %v, want %v", err, want)
	}
	snap := m.snapshot()
	if snap["Authorize"] != 1 {
		t.Errorf("expected 1 Authorize call, got %d", snap["Authorize"])
	}
}

func TestMockServiceSecurityManager_LogAudit(t *testing.T) {
	t.Parallel()

	called := false
	m := &MockServiceSecurityManager{
		LogAuditFunc: func(_ string, _ ...any) {
			called = true
		},
	}
	m.LogAudit("action", "arg1", "arg2")
	if !called {
		t.Error("LogAuditFunc was not called")
	}
	snap := m.snapshot()
	if snap["LogAudit"] != 1 {
		t.Errorf("expected 1 LogAudit call, got %d", snap["LogAudit"])
	}
}

func TestMockServiceSecurityManager_TerminalLock(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		TerminalLockFunc: func() {},
	}
	m.TerminalLock()
	snap := m.snapshot()
	if snap["TerminalLock"] != 1 {
		t.Errorf("expected 1 TerminalLock call, got %d", snap["TerminalLock"])
	}
}

func TestMockServiceSecurityManager_TerminalUnlock(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		TerminalUnlockFunc: func() {},
	}
	m.TerminalUnlock()
	snap := m.snapshot()
	if snap["TerminalUnlock"] != 1 {
		t.Errorf("expected 1 TerminalUnlock call, got %d", snap["TerminalUnlock"])
	}
}

func TestMockServiceSecurityManager_Prompt(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		PromptFunc: func(_ string) {},
	}
	m.Prompt("message")
	if m.lastPrompt != "message" {
		t.Errorf("lastPrompt = %q, want %q", m.lastPrompt, "message")
	}
	snap := m.snapshot()
	if snap["Prompt"] != 1 {
		t.Errorf("expected 1 Prompt call, got %d", snap["Prompt"])
	}
}

func TestMockServiceSecurityManager_Warn(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		WarnFunc: func(_ string) {},
	}
	m.Warn("warning")
	if m.lastWarn != "warning" {
		t.Errorf("lastWarn = %q, want %q", m.lastWarn, "warning")
	}
	snap := m.snapshot()
	if snap["Warn"] != 1 {
		t.Errorf("expected 1 Warn call, got %d", snap["Warn"])
	}
}

func TestMockServiceSecurityManager_Confirm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockServiceSecurityManager{
		ConfirmFunc: func(_ context.Context, _ string) (bool, error) {
			return true, nil
		},
	}
	got, err := m.Confirm(ctx, "proceed?")
	if !got {
		t.Error("Confirm should return true")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["Confirm"] != 1 {
		t.Errorf("expected 1 Confirm call, got %d", snap["Confirm"])
	}
}

func TestMockServiceSecurityManager_ReadLine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockServiceSecurityManager{
		ReadLineFunc: func(_ context.Context) (string, error) {
			return "input", nil
		},
	}
	got, err := m.ReadLine(ctx)
	if got != "input" {
		t.Errorf("ReadLine = %q, want %q", got, "input")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["ReadLine"] != 1 {
		t.Errorf("expected 1 ReadLine call, got %d", snap["ReadLine"])
	}
}

func TestMockServiceSecurityManager_IsCommandAllowed(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		IsCommandAllowedFunc: func(_ string) bool {
			return true
		},
	}
	if !m.IsCommandAllowed("ls") {
		t.Error("IsCommandAllowed should return true")
	}
	snap := m.snapshot()
	if snap["IsCommandAllowed"] != 1 {
		t.Errorf("expected 1 IsCommandAllowed call, got %d", snap["IsCommandAllowed"])
	}
}

func TestMockServiceSecurityManager_IsBypassActive(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		IsBypassActiveFunc: func() bool {
			return false
		},
	}
	if m.IsBypassActive() {
		t.Error("IsBypassActive should return false")
	}
	snap := m.snapshot()
	if snap["IsBypassActive"] != 1 {
		t.Errorf("expected 1 IsBypassActive call, got %d", snap["IsBypassActive"])
	}
}

func TestMockServiceSecurityManager_Close(t *testing.T) {
	t.Parallel()

	m := &MockServiceSecurityManager{
		CloseFunc: func() error {
			return nil
		},
	}
	if err := m.Close(); err != nil {
		t.Errorf("Close unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["Close"] != 1 {
		t.Errorf("expected 1 Close call, got %d", snap["Close"])
	}
}

func TestMockServiceSecurityManager_Close_Error(t *testing.T) {
	t.Parallel()

	want := errors.New("close failed")
	m := &MockServiceSecurityManager{
		CloseFunc: func() error {
			return want
		},
	}
	if err := m.Close(); err != want {
		t.Errorf("Close error = %v, want %v", err, want)
	}
	snap := m.snapshot()
	if snap["Close"] != 1 {
		t.Errorf("expected 1 Close call, got %d", snap["Close"])
	}
}

// assertNilFuncStrErr validates the (string, error) zero-value return path
// for MockServiceSecurityManager methods when their Func field is nil.
func assertNilFuncStrErr(t *testing.T, got string, err error) {
	t.Helper()
	if got != "" {
		t.Errorf("nil func: got %q, want empty", got)
	}
	if err != nil {
		t.Errorf("nil func: unexpected error %v", err)
	}
}

// assertNilFuncBoolErr validates the (bool, error) zero-value return path.
func assertNilFuncBoolErr(t *testing.T, got bool, err error) {
	t.Helper()
	if got {
		t.Error("nil func: got true, want false")
	}
	if err != nil {
		t.Errorf("nil func: unexpected error %v", err)
	}
}

// assertNilFuncBool validates the bool zero-value return path.
func assertNilFuncBool(t *testing.T, got bool) {
	t.Helper()
	if got {
		t.Error("nil func: got true, want false")
	}
}

// assertNilFuncErr validates the error zero-value return path.
func assertNilFuncErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("nil func: unexpected error %v", err)
	}
}

// TestMockServiceSecurityManager_NilFuncs exercises every method's zero-value
// return path when its Func field is nil. Existing tests only cover the
// fn != nil branch.
func TestMockServiceSecurityManager_NilFuncs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(m *MockServiceSecurityManager)
	}{
		{
			name: "IsPathSafe_nil_func",
			call: func(m *MockServiceSecurityManager) {
				got, err := m.IsPathSafe("/p")
				assertNilFuncStrErr(t, got, err)
			},
		},
		{
			name: "IsPathWritable_nil_func",
			call: func(m *MockServiceSecurityManager) {
				got, err := m.IsPathWritable("/p")
				assertNilFuncStrErr(t, got, err)
			},
		},
		{
			name: "Authorize_nil_func",
			call: func(m *MockServiceSecurityManager) {
				got, err := m.Authorize(context.Background(), "l", "d", "r", true)
				assertNilFuncBoolErr(t, got, err)
			},
		},
		{
			name: "Confirm_nil_func",
			call: func(m *MockServiceSecurityManager) {
				got, err := m.Confirm(context.Background(), "msg")
				assertNilFuncBoolErr(t, got, err)
			},
		},
		{
			name: "ReadLine_nil_func",
			call: func(m *MockServiceSecurityManager) {
				got, err := m.ReadLine(context.Background())
				assertNilFuncStrErr(t, got, err)
			},
		},
		{
			name: "IsCommandAllowed_nil_func",
			call: func(m *MockServiceSecurityManager) {
				assertNilFuncBool(t, m.IsCommandAllowed("ls"))
			},
		},
		{
			name: "IsBypassActive_nil_func",
			call: func(m *MockServiceSecurityManager) {
				assertNilFuncBool(t, m.IsBypassActive())
			},
		},
		{
			name: "Close_nil_func",
			call: func(m *MockServiceSecurityManager) {
				assertNilFuncErr(t, m.Close())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockServiceSecurityManager{}
			tt.call(m)
		})
	}
}

// ---------------------------------------------------------------------------
// I. mockServiceAgent (4 hand-rolled spy methods)
// ---------------------------------------------------------------------------

func TestMockServiceAgent_Chat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}
	m := &MockServiceAgent{
		ChatFunc: func(_ context.Context, _ *ports.Session, _ string) error {
			return nil
		},
	}
	err := m.Chat(ctx, sess, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["Chat"] != 1 {
		t.Errorf("expected 1 Chat call, got %d", snap["Chat"])
	}
}

func TestMockServiceAgent_Chat_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}
	want := errors.New("chat failed")
	m := &MockServiceAgent{
		ChatFunc: func(_ context.Context, _ *ports.Session, _ string) error {
			return want
		},
	}
	err := m.Chat(ctx, sess, "hello")
	if err != want {
		t.Errorf("Chat error = %v, want %v", err, want)
	}
	snap := m.snapshot()
	if snap["Chat"] != 1 {
		t.Errorf("expected 1 Chat call, got %d", snap["Chat"])
	}
}

func TestMockServiceAgent_SetLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockServiceAgent{
		SetLimitsFunc: func(_ context.Context, _, _, _ int) error {
			return nil
		},
	}
	err := m.SetLimits(ctx, 5, 1000, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["SetLimits"] != 1 {
		t.Errorf("expected 1 SetLimits call, got %d", snap["SetLimits"])
	}
}

func TestMockServiceAgent_SetLimits_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("limits invalid")
	m := &MockServiceAgent{
		SetLimitsFunc: func(_ context.Context, _, _, _ int) error {
			return want
		},
	}
	err := m.SetLimits(ctx, -1, 1000, 10)
	if err != want {
		t.Errorf("SetLimits error = %v, want %v", err, want)
	}
	snap := m.snapshot()
	if snap["SetLimits"] != 1 {
		t.Errorf("expected 1 SetLimits call, got %d", snap["SetLimits"])
	}
}

func TestMockServiceAgent_Subscribe(t *testing.T) {
	t.Parallel()

	sub := func(ctx context.Context, ev events.Event) {}
	m := &MockServiceAgent{
		SubscribeFunc: func(_ func(context.Context, events.Event)) {},
	}
	m.Subscribe(sub)
	if m.lastSubscribeHandler == nil {
		t.Error("lastSubscribeHandler should not be nil after Subscribe")
	}
	snap := m.snapshot()
	if snap["Subscribe"] != 1 {
		t.Errorf("expected 1 Subscribe call, got %d", snap["Subscribe"])
	}
}

func TestMockServiceAgent_Shutdown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockServiceAgent{
		ShutdownFunc: func(_ context.Context) error {
			return nil
		},
	}
	err := m.Shutdown(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := m.snapshot()
	if snap["Shutdown"] != 1 {
		t.Errorf("expected 1 Shutdown call, got %d", snap["Shutdown"])
	}
}

func TestMockServiceAgent_Shutdown_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("shutdown failed")
	m := &MockServiceAgent{
		ShutdownFunc: func(_ context.Context) error {
			return want
		},
	}
	err := m.Shutdown(ctx)
	if err != want {
		t.Errorf("Shutdown error = %v, want %v", err, want)
	}
	snap := m.snapshot()
	if snap["Shutdown"] != 1 {
		t.Errorf("expected 1 Shutdown call, got %d", snap["Shutdown"])
	}
}

// TestMockServiceAgent_NilFuncs exercises every method's zero-value return path
// when its Func field is nil. Existing tests only cover the fn != nil branch.
func TestMockServiceAgent_NilFuncs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(m *MockServiceAgent)
	}{
		{
			name: "Chat_nil_func",
			call: func(m *MockServiceAgent) {
				err := m.Chat(context.Background(), &ports.Session{ID: "nil-test"}, "hello")
				if err != nil {
					t.Errorf("Chat nil func: unexpected error %v", err)
				}
			},
		},
		{
			name: "SetLimits_nil_func",
			call: func(m *MockServiceAgent) {
				err := m.SetLimits(context.Background(), 5, 1000, 10)
				if err != nil {
					t.Errorf("SetLimits nil func: unexpected error %v", err)
				}
			},
		},
		{
			name: "Shutdown_nil_func",
			call: func(m *MockServiceAgent) {
				err := m.Shutdown(context.Background())
				if err != nil {
					t.Errorf("Shutdown nil func: unexpected error %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockServiceAgent{}
			tt.call(m)
		})
	}
}

// ---------------------------------------------------------------------------
// J. mockTurnsLogger (3 hand-rolled spy methods)
// ---------------------------------------------------------------------------

func TestMockTurnsLogger_HandleEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ev := events.TurnStatusEvent{}
	m := &MockTurnsLogger{
		HandleEventFunc: func(_ context.Context, _ events.Event) {},
	}
	m.HandleEvent(ctx, ev)
	snap := m.Snapshot()
	if snap["HandleEvent"] != 1 {
		t.Errorf("expected 1 HandleEvent call, got %d", snap["HandleEvent"])
	}
	if !reflect.DeepEqual(m.lastHandleEvent, ev) {
		t.Errorf("lastHandleEvent = %v, want %v", m.lastHandleEvent, ev)
	}
}

func TestMockTurnsLogger_Listen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := &MockTurnsLogger{
		ListenFunc: func(_ context.Context) error {
			return nil
		},
	}
	err := m.Listen(ctx)
	if err != nil {
		t.Errorf("Listen unexpected error: %v", err)
	}
	snap := m.Snapshot()
	if snap["Listen"] != 1 {
		t.Errorf("expected 1 Listen call, got %d", snap["Listen"])
	}
}

func TestMockTurnsLogger_Listen_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("listen failed")
	m := &MockTurnsLogger{
		ListenFunc: func(_ context.Context) error {
			return want
		},
	}
	err := m.Listen(ctx)
	if err != want {
		t.Errorf("Listen error = %v, want %v", err, want)
	}
	snap := m.Snapshot()
	if snap["Listen"] != 1 {
		t.Errorf("expected 1 Listen call, got %d", snap["Listen"])
	}
}

func TestMockTurnsLogger_Close(t *testing.T) {
	t.Parallel()

	m := &MockTurnsLogger{
		CloseFunc: func() error {
			return nil
		},
	}
	if err := m.Close(); err != nil {
		t.Errorf("Close unexpected error: %v", err)
	}
	snap := m.Snapshot()
	if snap["Close"] != 1 {
		t.Errorf("expected 1 Close call, got %d", snap["Close"])
	}
}

func TestMockTurnsLogger_Close_Error(t *testing.T) {
	t.Parallel()

	want := errors.New("close failed")
	m := &MockTurnsLogger{
		CloseFunc: func() error {
			return want
		},
	}
	if err := m.Close(); err != want {
		t.Errorf("Close error = %v, want %v", err, want)
	}
	snap := m.Snapshot()
	if snap["Close"] != 1 {
		t.Errorf("expected 1 Close call, got %d", snap["Close"])
	}
}

// TestMockTurnsLogger_NilFuncs exercises every method's zero-value return path
// when its Func field is nil. Existing tests only cover the fn != nil branch.
func TestMockTurnsLogger_NilFuncs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(m *MockTurnsLogger)
	}{
		{
			name: "Listen_nil_func",
			call: func(m *MockTurnsLogger) {
				err := m.Listen(context.Background())
				if err != nil {
					t.Errorf("Listen nil func: unexpected error %v", err)
				}
			},
		},
		{
			name: "Close_nil_func",
			call: func(m *MockTurnsLogger) {
				err := m.Close()
				if err != nil {
					t.Errorf("Close nil func: unexpected error %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockTurnsLogger{}
			tt.call(m)
		})
	}
}
