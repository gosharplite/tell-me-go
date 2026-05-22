// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"crypto/rand"
	"log/slog"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/require"
)

type mockFailingSummarizer struct{}

func (m *mockFailingSummarizer) Summarize(ctx context.Context, history []*llm.Content, focus string) (string, *llm.Metrics, error) {
	return "", nil, errors.New("summarizer failed")
}

func (m *mockFailingSummarizer) SummarizeRange(ctx context.Context, turns int, focus string) (string, *llm.Metrics, error) {
	return "", nil, errors.New("summarizer failed")
}

func TestGatekeeper_ErrorHandling(t *testing.T) {

	ctx := context.Background()
	req := &ports.ContextRequest{
		History:  make([]*llm.Content, 20),
		Metadata: ports.ContextMetadata{},
	}
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		req.History[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "msg"}}}
	}

	gatekeeper := sessctx.NewTokenGatekeeper(
		&agenttest.MockEstimator{},
		&mockFailingSummarizer{},
		sessctx.WithMaxTokens(100),
	)
	gatekeeper.Estimator.(*agenttest.MockEstimator).SetTokens(95)

	err := gatekeeper.Transform(ctx, req)
	if err == nil || err.Error() != "summarizer failed" {
		t.Errorf("Expected 'summarizer failed' error from handleSafetyPressure, got: %v", err)
	}
}

func TestManager_FirstMessageRoleError(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	tc.SetTokens(10)
	cs := sessctx.NewStrategy(tc)
	hm := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(cs, hm, nil, nil)

	err := cm.AddContent(context.Background(), &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "first"}}})
	if err == nil || err.Error() != "first message must be 'user', got 'model'" {
		t.Errorf("Expected role error, got: %v", err)
	}
}

func TestContextTransformers_HistoryRepairerEmpty(t *testing.T) {
	hr := &sessctx.HistoryRepairer{}
	req := &ports.ContextRequest{History: nil}
	err := hr.Transform(context.Background(), req)
	if err != nil {
		t.Errorf("Expected nil error for empty history, got: %v", err)
	}
}

func TestInternalTools_Errors(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	tc.SetTokens(10)
	cs := sessctx.NewStrategy(tc)
	hm := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(cs, hm, nil, nil)

	it := session.NewInternalTools(cm, &ports.NoOpLogger{})

	_, err := it.SummarizeHistory(context.Background(), map[string]interface{}{"turns": "invalid"}, nil)
	if err == nil {
		t.Error("Expected error from unmarshal in SummarizeHistory")
	}

	_, err = it.SummarizeHistory(context.Background(), map[string]interface{}{"turns": float64(1)}, nil)
	if err == nil || err.Error() != "terminal error: summarizer not initialized" {
		t.Errorf("Expected summarizer error, got: %v", err)
	}

	// Test turns <= 0 validation guard
	_, err = it.SummarizeHistory(context.Background(), map[string]interface{}{"turns": float64(0)}, nil)
	if err == nil || err.Error() != "invalid 'turns' parameter: must be > 0" {
		t.Errorf("Expected 'invalid turns' error, got: %v", err)
	}

	_, err = it.ManageHistory(context.Background(), map[string]interface{}{"index": "invalid"}, nil)
	if err == nil {
		t.Error("Expected error from unmarshal in ManageHistory")
	}

	// Test unsupported action validation guard
	_, err = it.ManageHistory(context.Background(), map[string]interface{}{"action": "bogus", "index": float64(0)}, nil)
	if err == nil || err.Error() != "unsupported action: bogus" {
		t.Errorf("Expected 'unsupported action' error, got: %v", err)
	}
}

type mockFailingChatter struct {
	err error
}

func (m *mockFailingChatter) Chat(ctx context.Context, session *ports.Session, prompt string) error {
	return nil
}
func (m *mockFailingChatter) Shutdown(ctx context.Context) error                    { return nil }
func (m *mockFailingChatter) Subscribe(handler func(context.Context, events.Event)) {}
func (m *mockFailingChatter) SetLimits(ctx context.Context, maxToolTurns, contextWindow, maxHistoryTurns int) error {
	return m.err
}
func (m *mockFailingChatter) SetCostTracker(tracker domain_pricing.CostTracker) {}
func (m *mockFailingChatter) GetName() string                                   { return "mock" }

type mockFailingCapturer struct{}

func (m *mockFailingCapturer) IsTTY(any) bool { return false }
func (m *mockFailingCapturer) CapturePrompt(context.Context, []string, ...ports.CaptureOption) (string, error) {
	return "", nil
}
func (m *mockFailingCapturer) Confirm(context.Context, string) (bool, error) {
	return false, nil
}
func (m *mockFailingCapturer) Close(context.Context) error { return nil }

type mockFailingUIRenderer struct{}

func (m *mockFailingUIRenderer) SetUseColor(bool)                                 {}
func (m *mockFailingUIRenderer) SetForceSpinner(bool)                             {}
func (m *mockFailingUIRenderer) LogTurnStatus(context.Context, events.TurnStatus) {}
func (m *mockFailingUIRenderer) StartSpinner(ctx context.Context) func()          { return func() {} }
func (m *mockFailingUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	return func() {}
}
func (m *mockFailingUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	return func() {}
}
func (m *mockFailingUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
}
func (m *mockFailingUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
}
func (m *mockFailingUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
}
func (m *mockFailingUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
}
func (m *mockFailingUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
}
func (m *mockFailingUIRenderer) IsTerminalContext() bool {
	return false
}
func (m *mockFailingUIRenderer) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {
}

func TestSessionManager_ConfigError(t *testing.T) {
	agentFactory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return &mockFailingChatter{err: errors.New("config failed")}, nil
	}

	o := session.NewSessionManager("", "", nil, io.Discard, io.Discard, agentFactory, nil, &mockFailingUIRenderer{}, clock.RealClock{}, rand.Reader)

	cfg := &config.Config{
		SelectedProvider: "test",
	}
	sc := session.NewSessionConfig("", false, 0, 0, false, "test prompt", cfg)

	ic := &mockFailingCapturer{}
	sd := session.NewSessionDependencies(&persistence.Paths{}, nil, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, nil, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

	err := o.Run(context.Background(), sc, sd, ic)
	if err == nil || err.Error() != "failed to apply configuration: config failed" {
		t.Errorf("Expected config failed error, got: %v", err)
	}
}

func TestTokenGatekeeper_NilSummarizer(t *testing.T) {
	ctx := context.Background()
	req := &ports.ContextRequest{
		History:  make([]*llm.Content, 20),
		Metadata: ports.ContextMetadata{},
	}
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		req.History[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "msg"}}}
	}

	gatekeeper := sessctx.NewTokenGatekeeper(
		&agenttest.MockEstimator{},
		nil,
		sessctx.WithMaxTokens(100000),
	)
	// Summarizer intentionally nil — tests the nil-guard path
	// 95% tokens triggers safety pressure (90% threshold) but stays under
	// the hard limit (MaxTokens - reserved = 100000 - min(1000, 10000) = 99000)
	gatekeeper.Estimator.(*agenttest.MockEstimator).SetTokens(95000)

	err := gatekeeper.Transform(ctx, req)
	require.ErrorIs(t, err, llm.ErrTerminal)
	require.True(t, req.Metadata.MaintenanceBlocked)
	require.Contains(t, err.Error(), "summarizer not initialized")
}

func TestTokenGatekeeper_EventPublish_Errors(t *testing.T) {
	// Create a mock event bus that always returns an error
	mockBus := &eventstest.TestEventBus{}
	mockBus.SetPublishErr(context.Canceled)

	// Create a strategy that will trigger warnings to force event publishing
	counter := &agenttest.MockTokenCounter{}
	counter.SetTokens(950)
	strategy := sessctx.NewStrategy(counter)

	gatekeeper := sessctx.NewTokenGatekeeper(
		strategy,
		nil,
		sessctx.WithMaxTokens(1000),
		sessctx.WithEvents(mockBus),
	)

	// We need a large payload to trigger the token limit warning
	largeText := ""
	for i := 0; i < 200; i++ {
		largeText += "token "
	}

	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: largeText}}},
		},
		Metadata: ports.ContextMetadata{},
	}

	// 1. Test Transform (Should fail when emitting warning)
	err := gatekeeper.Transform(context.Background(), req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTokenGatekeeper_FindSummarizableRange_ContextCancellation(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)

	gatekeeper := sessctx.NewTokenGatekeeper(
		strategy,
		nil,
		sessctx.WithMaxTokens(10),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Provide enough history to trigger a summarization check
	history := []*llm.Content{
		{Role: "system", Parts: []*llm.Part{{Text: "System prompt"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "Message 1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "Response 1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "Message 2"}}},
	}

	_, _, _, err := gatekeeper.FindSummarizableRange(ctx, history)
	require.ErrorIs(t, err, context.Canceled)
}
