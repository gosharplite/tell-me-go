// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// --- Mocks ---

type mockChatter struct {
	mock.Mock
}

func (m *mockChatter) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	args := m.Called(ctx, s, prompt)
	return args.Error(0)
}

func (m *mockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	args := m.Called(ctx, toolTurns, historyTokens, historyTurns)
	return args.Error(0)
}

func (m *mockChatter) SetTieredThreshold(ctx context.Context, threshold int) error {
	args := m.Called(ctx, threshold)
	return args.Error(0)
}

func (m *mockChatter) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}

func (m *mockChatter) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type mockUIRenderer struct {
	mock.Mock
}

func (m *mockUIRenderer) StartSpinner(ctx context.Context) func() {
	args := m.Called(ctx)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *mockUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	args := m.Called(ctx, status)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *mockUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	args := m.Called(ctx, status)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *mockUIRenderer) RenderResponse(content *llm.Content, showThoughts, rawOutput bool) {
	m.Called(content, showThoughts, rawOutput)
}

func (m *mockUIRenderer) LogTurnStatus(status events.TurnStatus) {
	m.Called(status)
}

func (m *mockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *mockUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.Called(calls, turn, maxTurns, showTools)
}

func (m *mockUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	m.Called(name, result, showTools)
}

func (m *mockUIRenderer) LogSystemMessage(msg string, level string) {
	m.Called(msg, level)
}

func (m *mockUIRenderer) SetUseColor(use bool) {
	m.Called(use)
}

func (m *mockUIRenderer) SetForceSpinner(force bool) {
	m.Called(force)
}

type mockHistoryRenderer struct {
	mock.Mock
}

func (m *mockHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	m.Called(w, h, n, options)
}

type mockCapturer struct {
	mock.Mock
}

func (m *mockCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *mockCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...ports.CaptureOption) (string, error) {
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

func (m *mockCapturer) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// --- Helpers ---

func (b *uiBridge) sync(ctx context.Context) {
	reply := make(chan struct{})
	b.handleEvent(ctx, syncEvent{reply: reply})
	select {
	case <-reply:
	case <-ctx.Done():
	case <-b.done:
	}
}

// --- Tests ---

func TestOrchestrator_Run_Success(t *testing.T) {
	defer goleak.VerifyNone(t)
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default())

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
	mChatter.On("Shutdown", mock.Anything).Return(nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	mChatter.AssertExpectations(t)
	mCapturer.AssertExpectations(t)
}

func TestUIBridge_HandleEvent(t *testing.T) {
	defer goleak.VerifyNone(t)
	tests := []struct {
		name     string
		event    events.Event
		setup    func(m *mockUIRenderer)
		preSetup func(b *uiBridge)
		verify   func(t *testing.T, b *uiBridge)
	}{
		{
			name: "TurnStatusEvent",
			event: events.TurnStatusEvent{
				Status: events.TurnStatus{SessionTurns: 1},
			},
			setup: func(m *mockUIRenderer) {
				m.On("LogTurnStatus", mock.Anything).Return()
			},
		},
		{
			name: "UsageMetricsEvent",
			event: events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{PromptTokens: 10},
				StartTime: time.Now(),
				Context:   context.Background(),
			},
			setup: func(m *mockUIRenderer) {
				m.On("LogUsage", mock.Anything, mock.Anything, "log.txt", mock.Anything).Return()
			},
		},
		{
			name: "ToolCallEvent",
			event: events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			},
			setup: func(m *mockUIRenderer) {
				m.On("LogToolCall", mock.Anything, 0, 5, true).Return()
			},
		},
		{
			name: "ToolResultEvent",
			event: events.ToolResultEvent{
				Name:   "test",
				Result: tools.ToolResult{Text: "result"},
			},
			setup: func(m *mockUIRenderer) {
				m.On("LogToolResult", "test", mock.Anything, true).Return()
			},
		},
		{
			name: "SystemMessageEvent",
			event: events.SystemMessageEvent{
				Message: "msg",
				Level:   "info",
			},
			setup: func(m *mockUIRenderer) {
				m.On("LogSystemMessage", "msg", "info").Return()
			},
		},
		{
			name: "StatusUpdate",
			event: events.StatusUpdate{
				Message: "updating",
				Level:   "info",
			},
			setup: func(m *mockUIRenderer) {
				m.On("LogSystemMessage", "updating", "info").Return()
			},
		},
		{
			name: "InferenceStartedEvent (Model)",
			event: events.InferenceStartedEvent{
				Model: "gpt-4o",
			},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Return(func() {})
			},
		},
		{
			name:  "InferenceStartedEvent (Empty)",
			event: events.InferenceStartedEvent{},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() {})
			},
		},
		{
			name:  "SummarizationStartedEvent",
			event: events.SummarizationStartedEvent{},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Return(func() {})
			},
		},
		{
			name: "ToolExecutionStartedEvent (Single)",
			event: events.ToolExecutionStartedEvent{
				ToolNames: []string{"search_files"},
			},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing [search_files]...").Return(func() {})
			},
		},
		{
			name: "ToolExecutionStartedEvent (Multiple)",
			event: events.ToolExecutionStartedEvent{
				ToolNames: []string{"list_files", "read_files"},
			},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools [list_files, read_files]...").Return(func() {})
			},
		},
		{
			name:  "ToolExecutionStartedEvent (Empty)",
			event: events.ToolExecutionStartedEvent{},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools...").Return(func() {})
			},
		},
		{
			name: "RetryWaitingEvent",
			event: events.RetryWaitingEvent{
				Duration: 5 * time.Second,
			},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithStatus", mock.Anything, " Retrying in 5s...").Return(func() {})
			},
		},
		{
			name:  "ConsentStartedEvent (Stops Spinner)",
			event: events.ConsentStartedEvent{},
			setup: func(m *mockUIRenderer) {
				m.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {})
			},
			preSetup: func(b *uiBridge) {
				// Start a spinner first
				b.handleEvent(context.Background(), events.InferenceStartedEvent{})
				b.sync(context.Background())
			},
			verify: func(t *testing.T, b *uiBridge) {
				b.sync(context.Background())
				// We can't easily check b.stopSpinner without a race,
				// but the mock expectations and logic should cover it.
			},
		},
		{
			name:  "ConsentFinishedEvent (Resumes Active Phase)",
			event: events.ConsentFinishedEvent{},
			preSetup: func(b *uiBridge) {
				// Set active phase via event
				b.handleEvent(context.Background(), events.InferenceStartedEvent{Model: "gpt-4o"})
				// Enter consent
				b.handleEvent(context.Background(), events.ConsentStartedEvent{})
				b.sync(context.Background())
			},
			setup: func(m *mockUIRenderer) {
				// Expect it to be started twice: once originally, once after consent
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Return(func() {}).Twice()
			},
		},
		{
			name: "ResponseEvent",
			event: events.ResponseEvent{
				Content: &llm.Content{Parts: []*llm.Part{{Text: "result"}}},
			},
			setup: func(m *mockUIRenderer) {
				m.On("RenderResponse", mock.Anything, true, false).Return()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mRenderer := new(mockUIRenderer)
			bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
			defer bridge.Cleanup()
			// Set up expectations BEFORE preSetup
			tt.setup(mRenderer)
			if tt.preSetup != nil {
				tt.preSetup(bridge)
			}

			bridge.handleEvent(context.Background(), tt.event)

			// Wait for the async actor loop to process the event
			bridge.sync(context.Background())

			mRenderer.AssertExpectations(t)
			if tt.verify != nil {
				tt.verify(t, bridge)
			}
		})
	}
}

func TestUIBridge_EnsureContext(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	t.Run("Returns existing context", func(t *testing.T) {
		type contextKey string
		const testKey contextKey = "key"
		ctx := context.WithValue(context.Background(), testKey, "value")
		result := bridge.ensureContext(ctx, "test")
		assert.Equal(t, ctx, result)
	})

	t.Run("Returns background context and logs warning if nil", func(t *testing.T) {
		mRenderer.On("LogSystemMessage", "test missing context", "warn").Once()
		var nilCtx context.Context
		result := bridge.ensureContext(nilCtx, "test")
		assert.NotNil(t, result)
		mRenderer.AssertExpectations(t)
	})
}

func TestOrchestrator_Run_Error(t *testing.T) {
	defer goleak.VerifyNone(t)
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default())

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(fmt.Errorf("chat error"))
	mChatter.On("Shutdown", mock.Anything).Return(nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chat error")

	mChatter.AssertExpectations(t)
}

func TestOrchestrator_Run_NoPrompt_WithLastN(t *testing.T) {
	defer goleak.VerifyNone(t)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return nil, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)

	params := RunParams{
		HomeDir:         "home",
		Version:         "1.0.0",
		Loader:          nil,
		SM:              nil,
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		AgentFactory:    factory,
		HistoryRenderer: mHistoryRenderer,
		UIRenderer:      mUIRenderer,
		Prompt:          "",
		LastN:           5,
		Config:          &config.Config{},
		Deps:            newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default()),
		Capturer:        mCapturer,
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mHistoryRenderer.On("Render", io.Discard, mHistory, 5, mock.Anything).Return()

	err := Run(context.Background(), params)
	require.NoError(t, err)

	mCapturer.AssertExpectations(t)
	mHistoryRenderer.AssertExpectations(t)
}

func TestOrchestrator_ApplyConfiguration_Error(t *testing.T) {
	defer goleak.VerifyNone(t)
	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer)
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)

	sCfg := &sessionConfig{
		Config: &config.Config{
			MaxToolTurns: 10,
		},
	}
	paths := &persistence.Paths{}
	pData := domain_pricing.PricingData{}

	mCapturer.On("IsTTY", mock.Anything).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, 10, mock.Anything, mock.Anything).Return(fmt.Errorf("limits error"))

	cleanup, err := orch.(*orchestrator).applyConfiguration(context.Background(), mChatter, sCfg, paths, pData, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits error")
	require.NotNil(t, cleanup)
	cleanup()
}

// --- Behavioral Sequence Testing ---

type behaviorTracker struct {
	sequence []string
}

func (t *behaviorTracker) record(name string) {
	t.sequence = append(t.sequence, name)
}

type behaviorMockChatter struct {
	mock.Mock
	tracker *behaviorTracker
}

func (m *behaviorMockChatter) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	m.tracker.record("Chatter.Chat")
	args := m.Called(ctx, s, prompt)
	return args.Error(0)
}

func (m *behaviorMockChatter) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	m.tracker.record("Chatter.SetLimits")
	args := m.Called(ctx, toolTurns, historyTokens, historyTurns)
	return args.Error(0)
}

func (m *behaviorMockChatter) SetTieredThreshold(ctx context.Context, threshold int) error {
	m.tracker.record("Chatter.SetTieredThreshold")
	args := m.Called(ctx, threshold)
	return args.Error(0)
}

func (m *behaviorMockChatter) Subscribe(sub func(context.Context, events.Event)) {
	m.tracker.record("Chatter.Subscribe")
	m.Called(sub)
}

func (m *behaviorMockChatter) Shutdown(ctx context.Context) error {
	m.tracker.record("Chatter.Shutdown")
	args := m.Called(ctx)
	return args.Error(0)
}

type behaviorMockHistoryRenderer struct {
	mock.Mock
	tracker *behaviorTracker
}

func (m *behaviorMockHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	m.tracker.record("HistoryRenderer.Render")
	m.Called(w, h, n, options)
}

type behaviorMockUIRenderer struct {
	mock.Mock
	tracker *behaviorTracker
}

func (m *behaviorMockUIRenderer) StartSpinner(ctx context.Context) func() {
	m.tracker.record("UIRenderer.StartSpinner")
	args := m.Called(ctx)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *behaviorMockUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	m.tracker.record("UIRenderer.StartSpinnerWithStatus")
	args := m.Called(ctx, status)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *behaviorMockUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	m.tracker.record("UIRenderer.StartSpinnerWithMetrics")
	args := m.Called(ctx, status)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *behaviorMockUIRenderer) RenderResponse(content *llm.Content, showThoughts, rawOutput bool) {
	m.tracker.record("UIRenderer.RenderResponse")
	m.Called(content, showThoughts, rawOutput)
}

func (m *behaviorMockUIRenderer) LogTurnStatus(status events.TurnStatus) {
	m.tracker.record("UIRenderer.LogTurnStatus")
	m.Called(status)
}

func (m *behaviorMockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.tracker.record("UIRenderer.LogUsage")
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *behaviorMockUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.tracker.record("UIRenderer.LogToolCall")
	m.Called(calls, turn, maxTurns, showTools)
}

func (m *behaviorMockUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	m.tracker.record("UIRenderer.LogToolResult")
	m.Called(name, result, showTools)
}

func (m *behaviorMockUIRenderer) LogSystemMessage(msg string, level string) {
	m.tracker.record("UIRenderer.LogSystemMessage")
	m.Called(msg, level)
}

func (m *behaviorMockUIRenderer) SetUseColor(use bool) {
	m.tracker.record("UIRenderer.SetUseColor")
	m.Called(use)
}

func (m *behaviorMockUIRenderer) SetForceSpinner(force bool) {
	m.tracker.record("UIRenderer.SetForceSpinner")
	m.Called(force)
}

type behaviorMockCapturer struct {
	mock.Mock
	tracker *behaviorTracker
}

func (m *behaviorMockCapturer) IsTTY(v any) bool {
	m.tracker.record("Capturer.IsTTY")
	args := m.Called(v)
	return args.Bool(0)
}

func (m *behaviorMockCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...ports.CaptureOption) (string, error) {
	m.tracker.record("Capturer.CapturePrompt")
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

func (m *behaviorMockCapturer) Close(ctx context.Context) error {
	m.tracker.record("Capturer.Close")
	args := m.Called(ctx)
	return args.Error(0)
}

func TestOrchestrator_Run_BehaviorSequence(t *testing.T) {
	defer goleak.VerifyNone(t)
	tracker := &behaviorTracker{}
	mChatter := &behaviorMockChatter{tracker: tracker}
	mCapturer := &behaviorMockCapturer{tracker: tracker}
	mHistoryRenderer := &behaviorMockHistoryRenderer{tracker: tracker}
	mUIRenderer := &behaviorMockUIRenderer{tracker: tracker}

	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		tracker.record("AgentFactory")
		return mChatter, nil
	}

	params := RunParams{
		HomeDir:         "home",
		Version:         "1.0.0",
		Loader:          nil,
		SM:              nil,
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		AgentFactory:    factory,
		HistoryRenderer: mHistoryRenderer,
		UIRenderer:      mUIRenderer,
		Prompt:          "hello",
		LastN:           5,
		Config: &config.Config{
			Model:            "model",
			Mode:             "mode",
			SelectedProvider: "provider",
		},
		Deps:     newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default()),
		Capturer: mCapturer,
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mHistoryRenderer.On("Render", io.Discard, mHistory, 5, mock.Anything).Return()
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
	mChatter.On("Shutdown", mock.Anything).Return(nil)

	// Execute high-level Run function to cover it
	err := Run(context.Background(), params)
	require.NoError(t, err)

	expectedSequence := []string{
		"Capturer.IsTTY",             // Initial check in Run
		"HistoryRenderer.Render",     // Rendering history because LastN > 0
		"AgentFactory",               // Creating the agent
		"Capturer.IsTTY",             // Check in setupUIRendering
		"UIRenderer.SetUseColor",     // Config UI
		"Chatter.Subscribe",          // Connect UI events
		"Chatter.SetLimits",          // Apply constraints
		"Chatter.SetTieredThreshold", // Apply cost threshold
		"Chatter.Chat",               // Start conversation
		"Chatter.Shutdown",           // Cleanup
	}

	assert.Equal(t, expectedSequence, tracker.sequence, "Orchestrator must follow exact coordination sequence")

	mChatter.AssertExpectations(t)
	mCapturer.AssertExpectations(t)
	mUIRenderer.AssertExpectations(t)
	mHistoryRenderer.AssertExpectations(t)
}

func TestSessionDependencies_Accessors(t *testing.T) {
	paths := &persistence.Paths{}
	deps := &sessionDependencies{
		Paths: paths,
	}

	require.Equal(t, paths, deps.GetPaths())
	require.Nil(t, deps.GetPricingOverrides())
	require.Nil(t, deps.GetGateway())
	require.Nil(t, deps.GetRegistry())
	require.Nil(t, deps.GetSecurityManager())
	require.Nil(t, deps.GetEventBus())
	require.Nil(t, deps.GetTracker())
	require.Equal(t, domain_pricing.PricingData{}, deps.GetPricingData())
	require.Nil(t, deps.GetHistoryManager())
	require.Nil(t, deps.GetLogger())
}

func TestOrchestrator_AgentFactory_Error(t *testing.T) {
	// Create an orchestrator with a failing factory
	o := &orchestrator{
		AgentFactory: func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
			return nil, fmt.Errorf("factory failed")
		},
		Stderr: io.Discard, // Prevent spam
		Stdout: io.Discard,
	}

	deps := &sessionDependencies{
		Paths:          &persistence.Paths{},
		HistoryManager: new(mockHistoryManager),
	}
	sc := &sessionConfig{Config: &config.Config{}}

	mCapturer := new(mockCapturer)
	mCapturer.On("IsTTY", mock.Anything).Return(true)

	err := o.Run(context.Background(), sc, deps, mCapturer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "factory failed")
}

func TestOrchestrator_Rollback(t *testing.T) {
	defer goleak.VerifyNone(t)
	mHistory := &mockHistoryManager{
		contents: make([]*llm.Content, 4), // 2 turns
	}
	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer)

	tests := []struct {
		name          string
		backN         int
		rollbackErr   error
		expectedCalls int
		wantErr       bool
	}{
		{
			name:          "rollback 1 turn",
			backN:         1,
			expectedCalls: 1,
			wantErr:       false,
		},
		{
			name:          "rollback 0 turns",
			backN:         0,
			expectedCalls: 0,
			wantErr:       false,
		},
		{
			name:          "rollback error propagation",
			backN:         1,
			rollbackErr:   fmt.Errorf("disk failure"),
			expectedCalls: 1,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mHistory.rollbackErr = tt.rollbackErr
			sCfg := &sessionConfig{BackN: tt.backN}
			deps := &sessionDependencies{HistoryManager: mHistory}
			err := orch.Rollback(context.Background(), sCfg, deps)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.rollbackErr != nil {
					assert.Contains(t, err.Error(), tt.rollbackErr.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRun_Routing(t *testing.T) {
	defer goleak.VerifyNone(t)
	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	mCapturer := new(mockCapturer)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	factory := func(mChatter ports.Chatter) func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
			return mChatter, nil
		}
	}

	setupParams := func(mHistory ports.HistoryManager, mChatter ports.Chatter) RunParams {
		return RunParams{
			HomeDir:         "home",
			Version:         "1.0.0",
			Stdout:          io.Discard,
			Stderr:          io.Discard,
			AgentFactory:    factory(mChatter),
			HistoryRenderer: mHistoryRenderer,
			UIRenderer:      mUIRenderer,
			Deps:            newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default()),
			Capturer:        mCapturer,
			Config: &config.Config{
				Model: "model",
				Mode:  "mode",
			},
		}
	}

	t.Run("Rollback only (no prompt)", func(t *testing.T) {
		mHistory := &mockHistoryManager{
			contents: make([]*llm.Content, 4), // 2 turns
		}
		mChatter := new(mockChatter)
		p := setupParams(mHistory, mChatter)
		p.BackN = 1
		p.Prompt = ""

		mCapturer.On("IsTTY", io.Discard).Return(true).Once()

		err := Run(context.Background(), p)
		assert.NoError(t, err)
		assert.Equal(t, 2, len(mHistory.contents)) // 1 turn removed (2 messages)
		mChatter.AssertNotCalled(t, "Chat", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Rollback and Chat", func(t *testing.T) {
		mHistory := &mockHistoryManager{
			contents: make([]*llm.Content, 4), // 2 turns
		}
		mChatter := new(mockChatter)
		p := setupParams(mHistory, mChatter)
		p.BackN = 1
		p.Prompt = "hello"

		mCapturer.ExpectedCalls = nil
		mCapturer.On("IsTTY", io.Discard).Return(true)
		mUIRenderer.On("SetUseColor", true).Return()
		mChatter.On("Subscribe", mock.Anything).Return()
		mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
		mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
		mChatter.On("Shutdown", mock.Anything).Return(nil)

		err := Run(context.Background(), p)
		assert.NoError(t, err)
	})

	t.Run("Rollback aborts on error", func(t *testing.T) {
		mHistory := &mockHistoryManager{
			contents:    make([]*llm.Content, 4), // 2 turns
			rollbackErr: fmt.Errorf("rollback failed"),
		}
		mChatter := new(mockChatter)
		p := setupParams(mHistory, mChatter)
		p.BackN = 1
		p.Prompt = "hello"

		mCapturer.ExpectedCalls = nil
		mCapturer.On("IsTTY", io.Discard).Return(true)

		err := Run(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rollback failed")

		// Verify that Chatter.Chat was NOT called
		mChatter.AssertNotCalled(t, "Chat", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestUIBridge_Concurrency(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	// Setup mocks with Maybe() to handle concurrent calls safely
	mRenderer.On("StartSpinner", mock.Anything).Return(func() {}).Maybe()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogTurnStatus", mock.Anything).Return().Maybe()
	mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogUsage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogToolCall", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogToolResult", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	ctx := context.Background()
	var wg sync.WaitGroup
	const iterations = 1000
	start := make(chan struct{})

	// Fire InferenceStartedEvent and ResponseEvent simultaneously
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			bridge.handleEvent(ctx, events.InferenceStartedEvent{})
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			bridge.handleEvent(ctx, events.ResponseEvent{
				Content: &llm.Content{},
			})
		}
	}()

	// Fire other events to simulate real event bus behavior and increase noise
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			bridge.handleEvent(ctx, events.TurnStarted{})
			bridge.handleEvent(ctx, events.TurnStatusEvent{Status: events.TurnStatus{}})
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			bridge.handleEvent(ctx, events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{},
				StartTime: time.Now(),
				Context:   ctx,
			})
			bridge.handleEvent(ctx, events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			})
		}
	}()

	close(start)
	wg.Wait()
	bridge.sync(ctx)
}

func TestUIBridge_LogicalRace(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	// StartSpinnerWithStatus should NOT be called because ResponseEvent is already rendering
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return()

	ctx := context.Background()

	// 1. Mark as rendering via ResponseEvent
	bridge.handleEvent(ctx, events.ResponseEvent{
		Content: &llm.Content{},
	})

	// 2. Try to start a spinner (should be suppressed)
	bridge.handleEvent(ctx, events.InferenceStartedEvent{})

	bridge.sync(ctx) // Wait for actor loop

	// Verification
	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)
}

func TestUIBridge_AbortedTurn_SpinnerCleanup(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	spinnerStopped := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() { close(spinnerStopped) })

	// Start Inference
	bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})

	// Force new turn before ResponseEvent arrives (Simulates an abort/reset)
	bridge.handleEvent(context.Background(), events.TurnStarted{})

	bridge.sync(context.Background()) // Wait for actor loop

	select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Expected stopSpinner to be called during TurnStarted to prevent resource leaks")
	}
}

func TestUIBridge_Retry_Spinner(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	// First attempt
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() {}).Once()
	bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
	bridge.sync(context.Background())

	// Response (e.g. error)
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Once()
	bridge.handleEvent(context.Background(), events.ResponseEvent{
		Content: &llm.Content{},
	})
	bridge.sync(context.Background())

	// Second attempt (Retry)
	// Now this SHOULD be called because RetryWaitingEvent resets isRendering.
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Retrying in 5s...").Return(func() {}).Once()
	bridge.handleEvent(context.Background(), events.RetryWaitingEvent{Duration: 5 * time.Second})
	bridge.sync(context.Background())

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_CleanupOnUnexpectedExit(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	spinnerStopped := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() { close(spinnerStopped) })

	// Start Inference
	bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
	bridge.sync(context.Background()) // Wait for actor loop to start the spinner

	// Simulate unexpected exit by calling Cleanup
	bridge.Cleanup()

	select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Expected stopSpinner to be called during Cleanup")
	}

	// Double cleanup should be safe
	bridge.Cleanup()
}

func TestOrchestrator_Run_ErrorPropagation(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name          string
		chatErr       error
		expectedError string
	}{
		{
			name:          "Context Deadline Exceeded",
			chatErr:       context.DeadlineExceeded,
			expectedError: context.DeadlineExceeded.Error(),
		},
		{
			name:          "Unauthorized (API token error)",
			chatErr:       fmt.Errorf("unauthorized: invalid API key"),
			expectedError: "unauthorized: invalid API key",
		},
		{
			name:          "Rate Limiting",
			chatErr:       fmt.Errorf("rate limit exceeded"),
			expectedError: "rate limit exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mChatter := new(mockChatter)
			mCapturer := new(mockCapturer)
			mHistory := new(mockHistoryManager)
			mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
			inframock.CleanupBus(t, mEventBus)

			factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
				return mChatter, nil
			}

			mHistoryRenderer := new(mockHistoryRenderer)
			mUIRenderer := new(mockUIRenderer)
			orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

			sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
				Model: "model",
				Mode:  "mode",
			})
			deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default())

			mCapturer.On("IsTTY", io.Discard).Return(true)
			mUIRenderer.On("SetUseColor", true).Return()

			spinnerStarted := make(chan struct{})
			spinnerStopped := make(chan struct{})
			mUIRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(args mock.Arguments) {
				close(spinnerStarted)
			}).Return(func() { close(spinnerStopped) })

			mChatter.On("Subscribe", mock.Anything).Run(func(args mock.Arguments) {
				sub := args.Get(0).(func(context.Context, events.Event))
				// Simulate spinner start before chat fails
				sub(context.Background(), events.InferenceStartedEvent{})
			}).Return()

			mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
			mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(tt.chatErr)
			mChatter.On("Shutdown", mock.Anything).Return(nil)

			err := orch.Run(context.Background(), sCfg, deps, mCapturer)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.expectedError)

			// Wait for the async actor to process the InferenceStartedEvent
			select {
			case <-spinnerStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("Timeout waiting for spinner to start")
			}

			select {
			case <-spinnerStopped:
			case <-time.After(2 * time.Second):
				t.Error("Expected spinner to be stopped via deferred Cleanup on error")
			}

			mChatter.AssertExpectations(t)
			mCapturer.AssertExpectations(t)
		})
	}
}

func TestUIBridge_SpinnerTransitions(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	// 1. Summarization starts
	stopSummarizationCalled := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Return(func() {
		close(stopSummarizationCalled)
	}).Once()

	bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})
	bridge.sync(context.Background()) // Wait for actor loop

	// 2. Inference starts (without previous response)
	stopInferenceCalled := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() {
		close(stopInferenceCalled)
	}).Once()

	bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
	bridge.sync(context.Background()) // Wait for actor loop

	// Verification
	select {
	case <-stopSummarizationCalled:
	case <-time.After(2 * time.Second):
		t.Error("Expected summarization spinner to be stopped before inference started")
	}

	// Cleanup remaining
	bridge.Cleanup()
	select {
	case <-stopInferenceCalled:
	case <-time.After(2 * time.Second):
		t.Error("Expected inference spinner to be stopped during cleanup")
	}

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SpinnerConcurrency(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt")
	defer bridge.Cleanup()

	var activeSpinners int32

	// Thread-safe mock setup
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		atomic.AddInt32(&activeSpinners, 1)
	}).Return(func() {
		atomic.AddInt32(&activeSpinners, -1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})
			} else {
				bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
			}
		}(i)
	}
	wg.Wait()
	bridge.sync(context.Background()) // Give actor loop time to process
	bridge.Cleanup()

	// Verify all spinners were eventually stopped
	assert.Equal(t, int32(0), atomic.LoadInt32(&activeSpinners), "Expected all spinners to be stopped")
}
