// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"flag"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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

func (m *mockChatter) Subscribe(sub func(events.Event)) {
	m.Called(sub)
}

func (m *mockChatter) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type mockUIRenderer struct {
	mock.Mock
}

func (m *mockUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
	args := m.Called(ctx, showThoughts, rawOutput)
	return args.Get(0).(chan<- *llm.Content), args.Get(1).(func() *llm.Content)
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

func (m *mockCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...CaptureOption) (string, error) {
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

// --- Tests ---

func TestOrchestrator_Run_Success(t *testing.T) {
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background())

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
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus)

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
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, true, true, false, true, "log.txt")

	tests := []struct {
		name  string
		event events.Event
		setup func()
	}{
		{
			name: "TurnStatusEvent",
			event: events.TurnStatusEvent{
				Status: events.TurnStatus{SessionTurns: 1},
			},
			setup: func() {
				mRenderer.On("LogTurnStatus", mock.Anything).Return()
			},
		},
		{
			name: "UsageMetricsEvent",
			event: events.UsageMetricsEvent{
				Metrics:   &llm.Metrics{PromptTokens: 10},
				StartTime: time.Now(),
				Context:   context.Background(),
			},
			setup: func() {
				mRenderer.On("LogUsage", mock.Anything, mock.Anything, "log.txt", mock.Anything).Return()
			},
		},
		{
			name: "ToolCallEvent",
			event: events.ToolCallEvent{
				Calls:    []*llm.FunctionCall{{Name: "test"}},
				Turn:     0,
				MaxTurns: 5,
			},
			setup: func() {
				mRenderer.On("LogToolCall", mock.Anything, 0, 5, true).Return()
			},
		},
		{
			name: "ToolResultEvent",
			event: events.ToolResultEvent{
				Name:   "test",
				Result: tools.ToolResult{Text: "result"},
			},
			setup: func() {
				mRenderer.On("LogToolResult", "test", mock.Anything, true).Return()
			},
		},
		{
			name: "SystemMessageEvent",
			event: events.SystemMessageEvent{
				Message: "msg",
				Level:   "info",
			},
			setup: func() {
				mRenderer.On("LogSystemMessage", "msg", "info").Return()
			},
		},
		{
			name: "StatusUpdate",
			event: events.StatusUpdate{
				Message: "updating",
				Level:   "info",
			},
			setup: func() {
				mRenderer.On("LogSystemMessage", "updating", "info").Return()
			},
		},
		{
			name: "ResponseStreamEvent",
			event: events.ResponseStreamEvent{
				Context: context.Background(),
				Stream:  make(chan *llm.Content),
			},
			setup: func() {
				uiCh := make(chan *llm.Content)
				var uiChSend chan<- *llm.Content = uiCh
				mRenderer.On("StreamResponse", mock.Anything, true, false).Return(uiChSend, func() *llm.Content { return &llm.Content{} })

				// Close the stream in background to avoid blocking
				go func() {
					close(uiCh)
				}()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()

			// For ResponseStreamEvent, we need to handle the channel closing
			if ev, ok := tt.event.(events.ResponseStreamEvent); ok {
				stream := make(chan *llm.Content)
				ev.Stream = stream
				close(stream)
				bridge.handleEvent(ev)
			} else {
				bridge.handleEvent(tt.event)
			}

			mRenderer.AssertExpectations(t)
		})
	}
}

func TestUIBridge_RelayStream(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, true, true, false, true, "log.txt")

	t.Run("Relays content", func(t *testing.T) {
		ctx := context.Background()
		stream := make(chan *llm.Content, 2)
		uiCh := make(chan *llm.Content, 2)

		content := &llm.Content{Parts: []*llm.Part{{Text: "hello"}}}
		stream <- content
		close(stream)

		bridge.relayStream(ctx, stream, uiCh)

		select {
		case received := <-uiCh:
			assert.Equal(t, content, received)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for content")
		}
	})

	t.Run("Handles context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		stream := make(chan *llm.Content)
		uiCh := make(chan *llm.Content)

		cancel()
		bridge.relayStream(ctx, stream, uiCh)
		// Should return immediately
	})
}

func TestUIBridge_EnsureContext(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, true, true, false, true, "log.txt")

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
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background())

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
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus)

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
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background())

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
		Deps:            newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus),
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

	err := orch.(*orchestrator).applyConfiguration(context.Background(), mChatter, sCfg, paths, pData, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits error")
}

func TestUIBridge_RelayStream_ContextCancelledDuringSend(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, true, true, false, true, "log.txt")

	ctx, cancel := context.WithCancel(context.Background())
	stream := make(chan *llm.Content)
	uiCh := make(chan *llm.Content) // Unbuffered

	// Blocks until the consumer (relayStream) picks it up, guaranteeing state
	go func() {
		stream <- &llm.Content{}
		cancel()
	}()

	bridge.relayStream(ctx, stream, uiCh)
	// Should return when context is cancelled
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

func (m *behaviorMockChatter) Subscribe(sub func(events.Event)) {
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

func (m *behaviorMockUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
	m.tracker.record("UIRenderer.StreamResponse")
	args := m.Called(ctx, showThoughts, rawOutput)
	return args.Get(0).(chan<- *llm.Content), args.Get(1).(func() *llm.Content)
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

type behaviorMockCapturer struct {
	mock.Mock
	tracker *behaviorTracker
}

func (m *behaviorMockCapturer) IsTTY(v any) bool {
	m.tracker.record("Capturer.IsTTY")
	args := m.Called(v)
	return args.Bool(0)
}

func (m *behaviorMockCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...CaptureOption) (string, error) {
	m.tracker.record("Capturer.CapturePrompt")
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

func TestOrchestrator_Run_BehaviorSequence(t *testing.T) {
	tracker := &behaviorTracker{}
	mChatter := &behaviorMockChatter{tracker: tracker}
	mCapturer := &behaviorMockCapturer{tracker: tracker}
	mHistoryRenderer := &behaviorMockHistoryRenderer{tracker: tracker}
	mUIRenderer := &behaviorMockUIRenderer{tracker: tracker}

	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background())

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
		Deps:     newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus),
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
	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	mCapturer := new(mockCapturer)
	mEventBus := events.NewSimpleEventBus(context.Background())

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
			Deps:            newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus),
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
