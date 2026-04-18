// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Tests ---

func TestSessionManager_Run_Success(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)
	mTurnsLogger := new(agenttest.MockTurnsLogger)
	orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), mTurnsLogger, new(agenttest.MockSessionProvider), nil)

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
	mChatter.On("Shutdown", mock.Anything).Return(nil)

	// Verify TurnsLogger interaction during Run
	// (SessionManager now subscribes it directly)
	mTurnsLogger.On("HandleEvent", mock.Anything, mock.Anything).Return().Maybe()

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	mChatter.AssertExpectations(t)
	mCapturer.AssertExpectations(t)
	mTurnsLogger.AssertExpectations(t)
}

func TestSessionManager_Run_Error(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

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

func TestSessionManager_Run_NoPrompt_WithLastN(t *testing.T) {
	t.Parallel()
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return nil, nil
	}

	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)

	params := session.RunParams{
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
		Deps:            session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil),
		Capturer:        mCapturer,
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mHistoryRenderer.On("Render", io.Discard, mHistory, 5, mock.Anything).Return()

	err := session.Run(context.Background(), params)
	require.NoError(t, err)

	mCapturer.AssertExpectations(t)
	mHistoryRenderer.AssertExpectations(t)
}

func TestSessionManager_ApplyConfiguration_Error(t *testing.T) {
	t.Parallel()
	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)

	sCfg := &session.SessionConfigInternal{
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

	deps := session.NewSessionDependencies(paths, nil, nil, nil, nil, nil, nil, pData, nil, nil, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

	bridge, err := session.AsSessionManagerInternal(orch).ApplyConfiguration(context.Background(), mChatter, sCfg, deps, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits error")
	require.NotNil(t, bridge)
	bridge.Wg().Done() // Manually satisfy constructor wg.Add(1) since Listen wasn't called
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

func (m *behaviorMockUIRenderer) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {
	m.tracker.record("UIRenderer.RenderHealthReport")
	m.Called(ctx, report)
}

func (m *behaviorMockUIRenderer) SetUseColor(use bool) {
	m.tracker.record("UIRenderer.SetUseColor")
	m.Called(use)
}

func (m *behaviorMockUIRenderer) SetForceSpinner(force bool) {
	m.tracker.record("UIRenderer.SetForceSpinner")
	m.Called(force)
}

func (m *behaviorMockUIRenderer) IsTerminalContext() bool {
	m.tracker.record("UIRenderer.IsTerminalContext")
	args := m.Called()
	return args.Bool(0)
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

func (m *behaviorMockCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	m.tracker.record("Capturer.CapturePrompt")
	callArgs := m.Called(ctx, args, opts)
	return callArgs.String(0), callArgs.Error(1)
}

func (m *behaviorMockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	m.tracker.record("Capturer.Confirm")
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}

func (m *behaviorMockCapturer) Close(ctx context.Context) error {
	m.tracker.record("Capturer.Close")
	args := m.Called(ctx)
	return args.Error(0)
}

func TestSessionManager_Run_BehaviorSequence(t *testing.T) {
	t.Parallel()
	tracker := &behaviorTracker{}
	mChatter := &behaviorMockChatter{tracker: tracker}
	mCapturer := &behaviorMockCapturer{tracker: tracker}
	mHistoryRenderer := &behaviorMockHistoryRenderer{tracker: tracker}
	mUIRenderer := &behaviorMockUIRenderer{tracker: tracker}

	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		tracker.record("AgentFactory")
		return mChatter, nil
	}

	params := session.RunParams{
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
		Deps:     session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil),
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
	err := session.Run(context.Background(), params)
	require.NoError(t, err)

	expectedSequence := []string{
		"Capturer.IsTTY",             // Initial check in Run
		"HistoryRenderer.Render",     // Rendering history because LastN > 0
		"AgentFactory",               // Creating the agent
		"Chatter.Subscribe",          // Connect TurnsLogger events
		"Capturer.IsTTY",             // Check in setupUIRendering
		"UIRenderer.SetUseColor",     // Config UI
		"Chatter.Subscribe",          // Connect UI events
		"Chatter.SetLimits",          // Apply constraints
		"Chatter.SetTieredThreshold", // Apply cost threshold
		"Chatter.Chat",               // Start conversation
		"UIRenderer.StartSpinnerWithStatus",
		"UIRenderer.StopSpinner", // [REFACTOR] Stop Consumer first because it finishes when Chat returns
		"Chatter.Shutdown",       // Stop Producers last
	}

	assert.Equal(t, expectedSequence, tracker.sequence, "SessionManager must follow exact coordination sequence")

	mChatter.AssertExpectations(t)
	mCapturer.AssertExpectations(t)
	mUIRenderer.AssertExpectations(t)
	mHistoryRenderer.AssertExpectations(t)
}

func TestSessionDependencies_Accessors(t *testing.T) {
	t.Parallel()
	paths := &persistence.Paths{}
	sessionProvider := new(agenttest.MockSessionProvider)
	deps := &session.SessionDependenciesInternal{
		Paths:           paths,
		SessionProvider: sessionProvider,
	}

	require.Equal(t, paths, deps.GetPaths())
	require.Equal(t, sessionProvider, deps.GetSessionProvider())
	require.Nil(t, deps.GetPricingOverrides())
	require.Nil(t, deps.GetGateway())
	regGot, regErr := deps.GetRegistry()
	require.Nil(t, regGot)
	require.NoError(t, regErr)
	require.Nil(t, deps.GetSecurityManager())
	require.Nil(t, deps.GetEventBus())
	require.Nil(t, deps.GetTracker())
	require.Equal(t, domain_pricing.PricingData{}, deps.GetPricingData())
	require.Nil(t, deps.GetHistoryManager())
	require.Nil(t, deps.GetLogger())
}

func TestSessionManager_AgentFactory_Error(t *testing.T) {
	t.Parallel()
	// Create an session.SessionManagerInternal with a failing factory
	o := &session.SessionManagerInternal{
		AgentFactory: func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
			return nil, fmt.Errorf("factory failed")
		},
		Stderr:        io.Discard, // Prevent spam
		Stdout:        io.Discard,
		Clock:         clock.RealClock{},
		EntropySource: rand.Reader,
	}

	deps := &session.SessionDependenciesInternal{
		Paths:           &persistence.Paths{},
		HistoryManager:  new(agenttest.MockHistoryManager),
		SessionProvider: new(agenttest.MockSessionProvider),
	}
	sc := &session.SessionConfigInternal{Config: &config.Config{}}

	mCapturer := new(agenttest.MockCapturer)
	mCapturer.On("IsTTY", mock.Anything).Return(true)

	err := o.Run(context.Background(), sc, deps, mCapturer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "factory failed")
}

func TestSessionManager_Rollback(t *testing.T) {
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
			mHistory := new(agenttest.MockHistoryManager)
			mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
			mHistoryRenderer := new(testutil.MockHistoryRenderer)
			mUIRenderer := new(testutil.MockUIRenderer)
			orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

			mHistory.SetRollbackErr(tt.rollbackErr)
			sCfg := &session.SessionConfigInternal{BackN: tt.backN}
			deps := &session.SessionDependenciesInternal{HistoryManager: mHistory, SessionProvider: new(agenttest.MockSessionProvider)}
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

	setupParams := func(mHistory ports.HistoryManager, mChatter ports.Chatter, mHistoryRenderer *testutil.MockHistoryRenderer, mUIRenderer *testutil.MockUIRenderer, mCapturer *agenttest.MockCapturer, mEventBus events.EventBus) session.RunParams {
		return session.RunParams{
			HomeDir:         "home",
			Version:         "1.0.0",
			Stdout:          io.Discard,
			Stderr:          io.Discard,
			AgentFactory:    factory(mChatter),
			HistoryRenderer: mHistoryRenderer,
			UIRenderer:      mUIRenderer,
			Deps:            session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil),
			Capturer:        mCapturer,
			Config: &config.Config{
				Model: "model",
				Mode:  "mode",
			},
		}
	}

	t.Run("Rollback only (no prompt)", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(testutil.MockHistoryRenderer)
		mUIRenderer := new(testutil.MockUIRenderer)
		mCapturer := new(agenttest.MockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		events.CleanupBus(t, mEventBus)

		mHistory := new(agenttest.MockHistoryManager)
		mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
		mChatter := new(agenttest.MockChatter)
		mChatter.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = ""

		mCapturer.On("IsTTY", io.Discard).Return(true).Once()

		err := session.Run(context.Background(), p)
		assert.NoError(t, err)
		assert.Equal(t, 2, mHistory.GetTotalEntries()) // 1 turn removed (2 messages)
		mChatter.AssertNotCalled(t, "Chat", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("Rollback and Chat", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(testutil.MockHistoryRenderer)
		mUIRenderer := new(testutil.MockUIRenderer)
		mCapturer := new(agenttest.MockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		events.CleanupBus(t, mEventBus)

		mHistory := new(agenttest.MockHistoryManager)
		mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
		mChatter := new(agenttest.MockChatter)
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

		err := session.Run(context.Background(), p)
		assert.NoError(t, err)
	})

	t.Run("Rollback aborts on error", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(testutil.MockHistoryRenderer)
		mUIRenderer := new(testutil.MockUIRenderer)
		mCapturer := new(agenttest.MockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		events.CleanupBus(t, mEventBus)

		mHistory := new(agenttest.MockHistoryManager)
		mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
		mHistory.SetRollbackErr(fmt.Errorf("rollback failed"))
		mChatter := new(agenttest.MockChatter)
		mChatter.On("Chat", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = "hello"

		mCapturer.On("IsTTY", io.Discard).Return(true)

		err := session.Run(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rollback failed")

		// Verify that Chatter.Chat was NOT called
		mChatter.AssertNotCalled(t, "Chat", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestSessionManager_Run_ErrorPropagation(t *testing.T) {
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
			mChatter := new(agenttest.MockChatter)
			mCapturer := new(agenttest.MockCapturer)
			mHistory := new(agenttest.MockHistoryManager)
			mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
			events.CleanupBus(t, mEventBus)

			factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
				return mChatter, nil
			}

			mHistoryRenderer := new(testutil.MockHistoryRenderer)
			mUIRenderer := new(testutil.MockUIRenderer)
			orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, clock.RealClock{}, rand.Reader)

			sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
				Model: "model",
				Mode:  "mode",
			})
			deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

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

func TestSessionManager_SessionID_Fallback(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	mClock := new(testutil.TestifyMockClock)
	mEntropy := new(testutil.MockEntropySource)

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mClock.On("Now").Return(fixedTime)
	// Entropy source fails
	mEntropy.On("Read", mock.Anything).Return(nil, 0, fmt.Errorf("entropy failure"))

	expectedSessionID := fmt.Sprintf("session-%d", fixedTime.UnixNano())

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

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

func TestSessionManager_SessionID_DeterministicEntropy(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	mClock := new(testutil.TestifyMockClock)
	mEntropy := new(testutil.MockEntropySource)

	fixedEntropy := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	mEntropy.On("Read", mock.Anything).Return(fixedEntropy, len(fixedEntropy), nil)

	expectedSessionID := "session-0102030405060708"

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

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

func TestSessionManager_SessionID_ShortRead_Fallback(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, mEventBus)

	mClock := new(testutil.TestifyMockClock)
	mEntropy := new(testutil.MockEntropySource)

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

	mHistoryRenderer := new(testutil.MockHistoryRenderer)
	mUIRenderer := new(testutil.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := session.NewSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default(), &ports.NoOpTurnsLogger{}, new(agenttest.MockSessionProvider), nil)

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
