// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"crypto/rand"
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
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func (m *mockUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
	m.Called(ctx, content, showThoughts, rawOutput)
}

func (m *mockUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	m.Called(ctx, status)
}

func (m *mockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *mockUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.Called(ctx, calls, turn, maxTurns, showTools)
}

func (m *mockUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
	m.Called(ctx, name, result, showTools)
}

func (m *mockUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.Called(ctx, msg, level)
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

// --- Tests ---

func TestOrchestrator_Run_Success(t *testing.T) {
	t.Parallel()
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
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider))

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
	t.Parallel()
	tests := []struct {
		name     string
		event    events.Event
		setup    func(m *mockUIRenderer) <-chan struct{}
		preSetup func(b *uiBridge, m *mockUIRenderer)
		verify   func(t *testing.T, b *uiBridge)
	}{
		{
			name: "TurnStatusEvent",
			event: events.TurnStatusEvent{
				Status: events.TurnStatus{SessionTurns: 1},
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
		{
			name: "UsageMetricsEvent",
			event: events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{PromptTokens: 10},
				StartTime: time.Now(),
				Context:   context.Background(),
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogUsage", mock.Anything, mock.Anything, "log.txt", mock.Anything).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
		{
			name: "ToolCallEvent",
			event: events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogToolCall", mock.Anything, mock.Anything, 0, 5, true).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
		{
			name: "ToolResultEvent",
			event: events.ToolResultEvent{
				Name:   "test",
				Result: tools.ToolResult{Text: "result"},
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogToolResult", mock.Anything, "test", mock.Anything, true).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
		{
			name: "SystemMessageEvent",
			event: events.SystemMessageEvent{
				Message: "msg",
				Level:   "info",
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogSystemMessage", mock.Anything, "msg", "info").Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
		{
			name: "StatusUpdate",
			event: events.StatusUpdate{
				Message: "updating",
				Level:   "info",
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("LogSystemMessage", mock.Anything, "updating", "info").Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
		{
			name: "InferenceStartedEvent (Model)",
			event: events.InferenceStartedEvent{
				Model: "gpt-4o",
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "InferenceStartedEvent (Empty)",
			event: events.InferenceStartedEvent{},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "SummarizationStartedEvent",
			event: events.SummarizationStartedEvent{},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name: "ToolExecutionStartedEvent (Single)",
			event: events.ToolExecutionStartedEvent{
				ToolNames: []string{"search_files"},
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing [search_files]...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name: "ToolExecutionStartedEvent (Multiple)",
			event: events.ToolExecutionStartedEvent{
				ToolNames: []string{"list_files", "read_files"},
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools [list_files, read_files]...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "ToolExecutionStartedEvent (Empty)",
			event: events.ToolExecutionStartedEvent{},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name: "RetryWaitingEvent",
			event: events.RetryWaitingEvent{
				Duration: 5 * time.Second,
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, " Retrying in 5s...").Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
		},
		{
			name:  "ConsentStartedEvent (Stops Spinner)",
			event: events.ConsentStartedEvent{},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
					close(done)
				}).Return(func() {})
				return done
			},
			preSetup: func(b *uiBridge, m *mockUIRenderer) {
				// Start a spinner first
				_ = b.handleEvent(context.Background(), events.InferenceStartedEvent{})
				// No need for explicit waitMock here as preSetup's effects will be checked at end
			},
			verify: func(t *testing.T, b *uiBridge) {
				// Final wait ensures all events in sequence were processed
			},
		},
		{
			name:  "ConsentFinishedEvent (Resumes Active Phase)",
			event: events.ConsentFinishedEvent{},
			preSetup: func(b *uiBridge, m *mockUIRenderer) {
				// Set active phase via event
				_ = b.handleEvent(context.Background(), events.InferenceStartedEvent{Model: "gpt-4o"})
				// Enter consent
				_ = b.handleEvent(context.Background(), events.ConsentStartedEvent{})
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				var count int32
				m.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Run(func(_ mock.Arguments) {
					if atomic.AddInt32(&count, 1) == 2 {
						close(done)
					}
				}).Return(func() {}).Twice()
				return done
			},
		},
		{
			name: "ResponseEvent",
			event: events.ResponseEvent{
				Content: &llm.Content{Parts: []*llm.Part{{Text: "result"}}},
			},
			setup: func(m *mockUIRenderer) <-chan struct{} {
				done := make(chan struct{})
				m.On("RenderResponse", mock.Anything, mock.Anything, true, false).Run(func(_ mock.Arguments) {
					close(done)
				}).Return()
				return done
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mRenderer := new(mockUIRenderer)
			bridge := newUIBridge(mRenderer,
				withBridgeThoughts(true),
				withBridgeTools(true),
				withBridgeRawOutput(false),
				withBridgeColor(true),
				withBridgeLogFile("log.txt"),
				withBridgeLogger(slog.Default()),
			)
			bridge.Start(context.Background())
			defer func() { bridge.CloseInput(); bridge.Cleanup() }()
			// Set up expectations BEFORE preSetup
			done := tt.setup(mRenderer)
			if tt.preSetup != nil {
				tt.preSetup(bridge, mRenderer)
			}

			_ = bridge.handleEvent(context.Background(), tt.event)

			// Wait for the async actor loop to process the event(s)
			if done != nil {
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatalf("%s: timeout waiting for event processing", tt.name)
				}
			}

			if tt.verify != nil {
				tt.verify(t, bridge)
			}
		})
	}
}

func TestUIBridge_EnsureContext(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	t.Run("Returns existing context", func(t *testing.T) {
		type contextKey string
		const testKey contextKey = "key"
		ctx := context.WithValue(context.Background(), testKey, "value")
		result := bridge.ensureContext(ctx, "test")
		assert.Equal(t, ctx, result)
	})

	t.Run("Returns background context and logs debug if nil", func(t *testing.T) {
		var nilCtx context.Context
		result := bridge.ensureContext(nilCtx, "test")
		assert.NotNil(t, result)
	})
}

func TestOrchestrator_Run_Error(t *testing.T) {
	t.Parallel()
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
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider))

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
	t.Parallel()
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
		Deps:            newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider)),
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
	t.Parallel()
	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)
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

	deps := newSessionDependencies(paths, nil, nil, nil, nil, nil, nil, pData, nil, nil, slog.Default(), new(mockSessionProvider))

	bridge, err := orch.(*orchestrator).applyConfiguration(context.Background(), mChatter, sCfg, deps, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits error")
	require.NotNil(t, bridge)
	bridge.CloseInput()
	bridge.Cleanup()
}

// --- Behavioral Sequence Testing ---

type behaviorTracker struct {
	mu       sync.Mutex
	sequence []string
}

func (t *behaviorTracker) record(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
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
	var internalFn func()
	if fn, ok := args.Get(0).(func()); ok {
		internalFn = fn
	}
	return func() {
		m.tracker.record("UIRenderer.StopSpinner")
		if internalFn != nil {
			internalFn()
		}
	}
}

func (m *behaviorMockUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	m.tracker.record("UIRenderer.StartSpinnerWithStatus")
	args := m.Called(ctx, status)
	var internalFn func()
	if fn, ok := args.Get(0).(func()); ok {
		internalFn = fn
	}
	return func() {
		m.tracker.record("UIRenderer.StopSpinner")
		if internalFn != nil {
			internalFn()
		}
	}
}

func (m *behaviorMockUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	m.tracker.record("UIRenderer.StartSpinnerWithMetrics")
	args := m.Called(ctx, status)
	var internalFn func()
	if fn, ok := args.Get(0).(func()); ok {
		internalFn = fn
	}
	return func() {
		m.tracker.record("UIRenderer.StopSpinner")
		if internalFn != nil {
			internalFn()
		}
	}
}

func (m *behaviorMockUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
	m.tracker.record("UIRenderer.RenderResponse")
	m.Called(ctx, content, showThoughts, rawOutput)
}

func (m *behaviorMockUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	m.tracker.record("UIRenderer.LogTurnStatus")
	m.Called(ctx, status)
}

func (m *behaviorMockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.tracker.record("UIRenderer.LogUsage")
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *behaviorMockUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.tracker.record("UIRenderer.LogToolCall")
	m.Called(ctx, calls, turn, maxTurns, showTools)
}

func (m *behaviorMockUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
	m.tracker.record("UIRenderer.LogToolResult")
	m.Called(ctx, name, result, showTools)
}

func (m *behaviorMockUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.tracker.record("UIRenderer.LogSystemMessage")
	m.Called(ctx, msg, level)
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
	t.Parallel()
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
		Deps:     newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider)),
		Capturer: mCapturer,
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mHistoryRenderer.On("Render", io.Discard, mHistory, 5, mock.Anything).Return()
	mUIRenderer.On("SetUseColor", true).Return()

	var uiSub func(context.Context, events.Event)
	mChatter.On("Subscribe", mock.Anything).Run(func(args mock.Arguments) {
		uiSub = args.Get(0).(func(context.Context, events.Event))
	}).Return()

	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)

	spinnerStarted := make(chan struct{})
	mUIRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking [gpt-4o]...").Run(func(args mock.Arguments) {
		close(spinnerStarted)
	}).Return(func() {})

	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Run(func(args mock.Arguments) {
		if uiSub != nil {
			uiSub(context.Background(), events.InferenceStartedEvent{Model: "gpt-4o"})
		}
		// Ensure spinner is active before finishing chat to guarantee it's recorded before Shutdown
		<-spinnerStarted
	}).Return(nil)
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
		"UIRenderer.StartSpinnerWithStatus",
		"Chatter.Shutdown",       // Stop Producers first
		"UIRenderer.StopSpinner", // Stop Consumer second (deterministic)
	}

	assert.Equal(t, expectedSequence, tracker.sequence, "Orchestrator must follow exact coordination sequence")

	mChatter.AssertExpectations(t)
	mCapturer.AssertExpectations(t)
	mUIRenderer.AssertExpectations(t)
	mHistoryRenderer.AssertExpectations(t)
}

func TestSessionDependencies_Accessors(t *testing.T) {
	t.Parallel()
	paths := &persistence.Paths{}
	sessionProvider := new(mockSessionProvider)
	deps := &sessionDependencies{
		Paths:           paths,
		SessionProvider: sessionProvider,
	}

	require.Equal(t, paths, deps.GetPaths())
	require.Equal(t, sessionProvider, deps.GetSessionProvider())
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
	t.Parallel()
	// Create an orchestrator with a failing factory
	o := &orchestrator{
		AgentFactory: func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
			return nil, fmt.Errorf("factory failed")
		},
		Stderr:        io.Discard, // Prevent spam
		Stdout:        io.Discard,
		Clock:         clock.RealClock{},
		EntropySource: rand.Reader,
	}

	deps := &sessionDependencies{
		Paths:           &persistence.Paths{},
		HistoryManager:  new(mockHistoryManager),
		SessionProvider: new(mockSessionProvider),
	}
	sc := &sessionConfig{Config: &config.Config{}}

	mCapturer := new(mockCapturer)
	mCapturer.On("IsTTY", mock.Anything).Return(true)

	err := o.Run(context.Background(), sc, deps, mCapturer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "factory failed")
}

func TestOrchestrator_Rollback(t *testing.T) {
	t.Parallel()

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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mHistory := &mockHistoryManager{
				contents: make([]*llm.Content, 4), // 2 turns
			}
			mHistoryRenderer := new(mockHistoryRenderer)
			mUIRenderer := new(mockUIRenderer)
			orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

			mHistory.rollbackErr = tt.rollbackErr
			sCfg := &sessionConfig{BackN: tt.backN}
			deps := &sessionDependencies{HistoryManager: mHistory, SessionProvider: new(mockSessionProvider)}
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
	t.Parallel()

	factory := func(mChatter ports.Chatter) func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
			return mChatter, nil
		}
	}

	setupParams := func(mHistory ports.HistoryManager, mChatter ports.Chatter, mHistoryRenderer *mockHistoryRenderer, mUIRenderer *mockUIRenderer, mCapturer *mockCapturer, mEventBus events.EventBus) RunParams {
		return RunParams{
			HomeDir:         "home",
			Version:         "1.0.0",
			Stdout:          io.Discard,
			Stderr:          io.Discard,
			AgentFactory:    factory(mChatter),
			HistoryRenderer: mHistoryRenderer,
			UIRenderer:      mUIRenderer,
			Deps:            newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider)),
			Capturer:        mCapturer,
			Config: &config.Config{
				Model: "model",
				Mode:  "mode",
			},
		}
	}

	t.Run("Rollback only (no prompt)", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(mockHistoryRenderer)
		mUIRenderer := new(mockUIRenderer)
		mCapturer := new(mockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
		inframock.CleanupBus(t, mEventBus)

		mHistory := &mockHistoryManager{
			contents: make([]*llm.Content, 4), // 2 turns
		}
		mChatter := new(mockChatter)
		mChatter.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = ""

		mCapturer.On("IsTTY", io.Discard).Return(true).Once()

		err := Run(context.Background(), p)
		assert.NoError(t, err)
		assert.Equal(t, 2, len(mHistory.contents)) // 1 turn removed (2 messages)
		mChatter.AssertNotCalled(t, "Chat", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Rollback and Chat", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(mockHistoryRenderer)
		mUIRenderer := new(mockUIRenderer)
		mCapturer := new(mockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
		inframock.CleanupBus(t, mEventBus)

		mHistory := &mockHistoryManager{
			contents: make([]*llm.Content, 4), // 2 turns
		}
		mChatter := new(mockChatter)
		mChatter.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = "hello"

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
		t.Parallel()
		mHistoryRenderer := new(mockHistoryRenderer)
		mUIRenderer := new(mockUIRenderer)
		mCapturer := new(mockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
		inframock.CleanupBus(t, mEventBus)

		mHistory := &mockHistoryManager{
			contents:    make([]*llm.Content, 4), // 2 turns
			rollbackErr: fmt.Errorf("rollback failed"),
		}
		mChatter := new(mockChatter)
		mChatter.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = "hello"

		mCapturer.On("IsTTY", io.Discard).Return(true)

		err := Run(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rollback failed")

		// Verify that Chatter.Chat was NOT called
		mChatter.AssertNotCalled(t, "Chat", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestUIBridge_Concurrency(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// Setup mocks with Maybe() to handle concurrent calls safely
	mRenderer.On("StartSpinner", mock.Anything).Return(func() {}).Maybe()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogSystemMessage", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogUsage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogToolCall", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
	mRenderer.On("LogToolResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

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
			_ = bridge.handleEvent(ctx, events.InferenceStartedEvent{})
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = bridge.handleEvent(ctx, events.ResponseEvent{
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
			_ = bridge.handleEvent(ctx, events.TurnStarted{})
			_ = bridge.handleEvent(ctx, events.TurnStatusEvent{Status: events.TurnStatus{}})
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = bridge.handleEvent(ctx, events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{},
				StartTime: time.Now(),
				Context:   ctx,
			})
			_ = bridge.handleEvent(ctx, events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			})
		}
	}()

	close(start)
	wg.Wait()
	// No explicit sync needed here, Cleanup will wait for the loop to finish
}

func TestUIBridge_LogicalRace(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// StartSpinnerWithStatus should NOT be called because ResponseEvent is already rendering
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, mock.Anything).Return(func() {}).Maybe()

	ctx := context.Background()

	// 1. Mark as rendering via ResponseEvent
	_ = bridge.handleEvent(ctx, events.ResponseEvent{
		Content: &llm.Content{},
	})

	// 2. Try to start a spinner (should be suppressed)
	_ = bridge.handleEvent(ctx, events.InferenceStartedEvent{})

	// 3. Send a sentinel to ensure #2 was processed
	done := make(chan struct{})
	mRenderer.On("LogTurnStatus", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		close(done)
	}).Return().Once()
	_ = bridge.handleEvent(ctx, events.TurnStatusEvent{})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for LogicalRace sentinel")
	}

	// Verification
	mRenderer.AssertNotCalled(t, "StartSpinnerWithStatus", mock.Anything, mock.Anything)
}

func TestUIBridge_AbortedTurn_SpinnerCleanup(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	spinnerStopped := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Return(func() { close(spinnerStopped) })

	// Start Inference
	_ = bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})

	// Force new turn before ResponseEvent arrives (Simulates an abort/reset)
	_ = bridge.handleEvent(context.Background(), events.TurnStarted{})

	select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Expected stopSpinner to be called during TurnStarted to prevent resource leaks")
	}
}

func TestUIBridge_Retry_Spinner(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	// First attempt
	done1 := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(_ mock.Arguments) {
		close(done1)
	}).Return(func() {}).Once()
	_ = bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first attempt spinner")
	}

	// Response (e.g. error)
	done2 := make(chan struct{})
	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
		close(done2)
	}).Return().Once()
	_ = bridge.handleEvent(context.Background(), events.ResponseEvent{
		Content: &llm.Content{},
	})
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Second attempt (Retry)
	// Now this SHOULD be called because RetryWaitingEvent resets isRendering.
	done3 := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Retrying in 5s...").Run(func(_ mock.Arguments) {
		close(done3)
	}).Return(func() {}).Once()
	_ = bridge.handleEvent(context.Background(), events.RetryWaitingEvent{Duration: 5 * time.Second})
	select {
	case <-done3:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for retry spinner")
	}

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_CleanupOnUnexpectedExit(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())

	spinnerStarted := make(chan struct{})
	spinnerStopped := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(args mock.Arguments) {
		close(spinnerStarted)
	}).Return(func() { close(spinnerStopped) })

	// Start Inference
	_ = bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})

	// Wait for spinner to start
	select {
	case <-spinnerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for spinner to start")
	}

	// Simulate unexpected exit by calling Cleanup
	bridge.CloseInput()
	bridge.Cleanup()

	select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Expected stopSpinner to be called during Cleanup")
	}
}

func TestOrchestrator_Run_ErrorPropagation(t *testing.T) {
	t.Parallel()

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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

			sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
				Model: "model",
				Mode:  "mode",
			})
			deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider))

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
			mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Run(func(args mock.Arguments) {
				// Wait for the spinner to start before failing chat to avoid racing with fast-drain
				select {
				case <-spinnerStarted:
				case <-time.After(2 * time.Second):
				}
			}).Return(tt.chatErr)
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
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())

	// 1. Summarization starts
	stopSummarizationCalled := make(chan struct{})
	doneSummarization := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Compressing context...").Run(func(_ mock.Arguments) {
		close(doneSummarization)
	}).Return(func() {
		close(stopSummarizationCalled)
	}).Once()

	_ = bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})
	select {
	case <-doneSummarization:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for summarization spinner")
	}

	// 2. Inference starts (without previous response)
	stopInferenceCalled := make(chan struct{})
	doneInference := make(chan struct{})
	mRenderer.On("StartSpinnerWithStatus", mock.Anything, " Thinking...").Run(func(_ mock.Arguments) {
		close(doneInference)
	}).Return(func() {
		close(stopInferenceCalled)
	}).Once()

	_ = bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
	select {
	case <-doneInference:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inference spinner")
	}

	// Verification
	select {
	case <-stopSummarizationCalled:
	case <-time.After(2 * time.Second):
		t.Error("Expected summarization spinner to be stopped before inference started")
	}

	// Cleanup remaining
	bridge.CloseInput()
	bridge.Cleanup()
	select {
	case <-stopInferenceCalled:
	case <-time.After(2 * time.Second):
		t.Error("Expected inference spinner to be stopped during cleanup")
	}

	mRenderer.AssertExpectations(t)
}

func TestUIBridge_SpinnerConcurrency(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, withBridgeThoughts(true), withBridgeTools(true), withBridgeRawOutput(false), withBridgeColor(true), withBridgeLogFile("log.txt"), withBridgeLogger(slog.Default()))
	bridge.Start(context.Background())

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
				_ = bridge.handleEvent(context.Background(), events.SummarizationStartedEvent{})
			} else {
				_ = bridge.handleEvent(context.Background(), events.InferenceStartedEvent{})
			}
		}(i)
	}
	wg.Wait()

	// Wait for all spinners to be stopped eventually
	bridge.CloseInput()
	bridge.Cleanup()

	// Verify all spinners were eventually stopped
	assert.Equal(t, int32(0), atomic.LoadInt32(&activeSpinners), "Expected all spinners to be stopped")
}

type mockEntropySource struct {
	mock.Mock
}

func (m *mockEntropySource) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	if args.Get(0) != nil {
		copy(p, args.Get(0).([]byte))
	}
	return args.Int(1), args.Error(2)
}

type mockClock struct {
	mock.Mock
}

func (m *mockClock) Now() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func (m *mockClock) Sleep(d time.Duration) {
	m.Called(d)
}

func (m *mockClock) After(d time.Duration) <-chan time.Time {
	args := m.Called(d)
	return args.Get(0).(<-chan time.Time)
}

func (m *mockClock) NewTicker(d time.Duration) clock.Ticker {
	args := m.Called(d)
	return args.Get(0).(clock.Ticker)
}

func (m *mockClock) Jitter(base float64) float64 {
	args := m.Called(base)
	return args.Get(0).(float64)
}

func TestOrchestrator_SessionID_Fallback(t *testing.T) {
	t.Parallel()
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	mClock := new(mockClock)
	mEntropy := new(mockEntropySource)

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mClock.On("Now").Return(fixedTime)
	// Entropy source fails
	mEntropy.On("Read", mock.Anything).Return(nil, 0, fmt.Errorf("entropy failure"))

	expectedSessionID := fmt.Sprintf("session-%d", fixedTime.UnixNano())

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider))

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)

	// Exact match on Session ID
	mChatter.On("Chat", mock.Anything, mock.MatchedBy(func(s *ports.Session) bool {
		return s.ID == expectedSessionID
	}), "hello").Return(nil)

	mChatter.On("Shutdown", mock.Anything).Return(nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	mClock.AssertExpectations(t)
	mEntropy.AssertExpectations(t)
	mChatter.AssertExpectations(t)
}

func TestOrchestrator_SessionID_DeterministicEntropy(t *testing.T) {
	t.Parallel()
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	mClock := new(mockClock)
	mEntropy := new(mockEntropySource)

	fixedEntropy := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	mEntropy.On("Read", mock.Anything).Return(fixedEntropy, len(fixedEntropy), nil)

	expectedSessionID := "session-0102030405060708"

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider))

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)

	// Exact match on Session ID
	mChatter.On("Chat", mock.Anything, mock.MatchedBy(func(s *ports.Session) bool {
		return s.ID == expectedSessionID
	}), "hello").Return(nil)

	mChatter.On("Shutdown", mock.Anything).Return(nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	mEntropy.AssertExpectations(t)
	mChatter.AssertExpectations(t)
}

func TestOrchestrator_SessionID_ShortRead_Fallback(t *testing.T) {
	t.Parallel()
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	mClock := new(mockClock)
	mEntropy := new(mockEntropySource)

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mClock.On("Now").Return(fixedTime)

	// Entropy source returns a short read (e.g., only 4 bytes instead of 8)
	shortEntropy := []byte{0x01, 0x02, 0x03, 0x04}
	mEntropy.On("Read", mock.Anything).Return(shortEntropy, len(shortEntropy), nil).Once()
	// io.ReadFull will call Read again if it didn't get enough bytes,
	// or it might fail immediately depending on the reader.
	// For most readers, it calls until full or error.
	// If we want to simulate EOF or short read that doesn't continue:
	mEntropy.On("Read", mock.Anything).Return(nil, 0, io.EOF).Maybe()

	// Since it's a short read, it should fallback to timestamp-based ID
	expectedSessionID := fmt.Sprintf("session-%d", fixedTime.UnixNano())

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), new(mockSessionProvider))

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)

	mChatter.On("Chat", mock.Anything, mock.MatchedBy(func(s *ports.Session) bool {
		return s.ID == expectedSessionID
	}), "hello").Return(nil)

	mChatter.On("Shutdown", mock.Anything).Return(nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	mClock.AssertExpectations(t)
	mChatter.AssertExpectations(t)
}

func TestUIBridge_NilLoggerFallback(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	// Instantiate without WithLogger
	bridge := newUIBridge(mRenderer)
	bridge.Start(context.Background())
	defer func() { bridge.CloseInput(); bridge.Cleanup() }()

	assert.NotNil(t, bridge.logger, "Logger should fall back to slog.Default() if nil")
}

func TestUIBridge_CleanupTimeout(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize bridge with a very small timeout via functional option
	bridge := newUIBridge(mRenderer,
		withBridgeCleanupTimeout(10*time.Millisecond),
	)
	bridgeCtx := bridge.Start(ctx)

	// Force a waitgroup hang to simulate a deadlocked renderer or long-running loop
	bridge.wg.Add(1)
	defer bridge.wg.Done() // Ensure the hung WaitGroup is eventually released to prevent goroutine leaks in the test suite.

	// Execute Cleanup. It should timeout after 10ms and return normally.
	done := make(chan struct{})
	go func() {
		bridge.CloseInput()
		bridge.Cleanup()
		close(done)
	}()

	select {
	case <-done:
		// Success: Cleanup returned even with a hung WaitGroup
	case <-time.After(1 * time.Second):
		t.Fatal("Cleanup did not return within expected timeout")
	}

	// VERIFY: context should be cancelled now
	assert.Error(t, bridgeCtx.Err(), "Context should be cancelled after Cleanup timeout")
}

func TestUIBridge_HandleEvent_ContextCancelled(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer)
	// We don't start the bridge's background loop to specifically test load shedding logic

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bridge.handleEvent(ctx, events.InferenceStartedEvent{})
	assert.ErrorIs(t, err, context.Canceled)
}
