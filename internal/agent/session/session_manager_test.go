// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"bytes"
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
	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Tests ---

func TestSessionManager_Run_Success(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	mTurnsLogger := &agenttest.MockTurnsLogger{}
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: mTurnsLogger, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }
	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error { return nil }
	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	// Verify TurnsLogger interaction during Run
	// (SessionManager now subscribes it directly)
	mTurnsLogger.HandleEventFunc = func(ctx context.Context, e events.Event) {}

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)
}

func TestSessionManager_Run_Error(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }
	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error { return fmt.Errorf("chat error") }
	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chat error")
}

func TestSessionManager_Run_NoPrompt_WithLastN(t *testing.T) {
	t.Parallel()
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return nil, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)

	params := session.RunParams{
		HomeDir:         "home",
		Version:         "1.0.0",
		SM:              nil,
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		AgentFactory:    factory,
		HistoryRenderer: mHistoryRenderer,
		UIRenderer:      mUIRenderer,
		Prompt:          "",
		LastN:           5,
		Config:          &config.Config{},
		Deps:            &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)},
		Capturer:        mCapturer,
	}

	mCapturer.IsTTYFn = func(v any) bool { return true }

	err := session.Run(context.Background(), params)
	require.NoError(t, err)

	calls, _ := mHistoryRenderer.Snapshot()
	if calls != 1 {
		t.Errorf("Render calls: got %d, want 1", calls)
	}
}

func TestSessionManager_ApplyConfiguration_Error(t *testing.T) {
	t.Parallel()
	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer)
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)

	sCfg := &session.SessionConfigInternal{
		Config: &config.Config{
			MaxToolTurns: 10,
		},
	}
	paths := &persistence.Paths{}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return fmt.Errorf("limits error") }

	deps := &agenttest.StubChatterComposer{Paths: paths, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	bridge, err := session.AsSessionManagerInternal(orch).ApplyConfiguration(context.Background(), mChatter, sCfg, deps, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits error")
	require.NotNil(t, bridge)
	bridge.AbortStart() // Manually satisfy constructor wg.Add(1) since Listen wasn't called
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

// trackedHistoryRenderer wraps agenttest.MockHistoryRenderer to record
// behavior sequence entries without testifying mocks.
type trackedHistoryRenderer struct {
	*agenttest.MockHistoryRenderer
	tracker *behaviorTracker
}

func (r *trackedHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	r.tracker.record("HistoryRenderer.Render")
	r.MockHistoryRenderer.Render(w, h, n, options)
}

func TestSessionManager_Run_BehaviorSequence(t *testing.T) {
	t.Parallel()
	tracker := &behaviorTracker{}
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mUIRenderer := new(agenttest.MockUIRenderer)

	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		tracker.record("AgentFactory")
		return mChatter, nil
	}

	params := session.RunParams{
		HomeDir:         "home",
		Version:         "1.0.0",
		SM:              nil,
		Stdout:          io.Discard,
		Stderr:          io.Discard,
		AgentFactory:    factory,
		HistoryRenderer: &trackedHistoryRenderer{MockHistoryRenderer: new(agenttest.MockHistoryRenderer), tracker: tracker},
		UIRenderer:      mUIRenderer,
		Prompt:          "hello",
		LastN:           5,
		Config: &config.Config{
			Model:            "model",
			Mode:             "mode",
			SelectedProvider: "provider",
		},
		Deps:     &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)},
		Capturer: mCapturer,
	}

	// Configure mocks with behavior-tracking function fields.
	mCapturer.IsTTYFn = func(v any) bool {
		tracker.record("Capturer.IsTTY")
		return true
	}

	mUIRenderer.SetUseColorFn = func(use bool) {
		tracker.record("UIRenderer.SetUseColor")
	}

	var uiSub func(context.Context, events.Event)
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {
		tracker.record("Chatter.Subscribe")
		uiSub = sub
	}

	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error {
		tracker.record("Chatter.SetLimits")
		return nil
	}

	spinnerStarted := make(chan struct{})
	mUIRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		if status == " Thinking [gpt-4o]..." {
			tracker.record("UIRenderer.StartSpinnerWithStatus")
			close(spinnerStarted)
			return func() {
				tracker.record("UIRenderer.StopSpinner")
			}
		}
		return func() {}
	}

	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error {
		tracker.record("Chatter.Chat")
		if uiSub != nil {
			uiSub(context.Background(), events.InferenceStartedEvent{Model: "gpt-4o"})
		}
		// Ensure spinner is active before finishing chat to guarantee it's recorded before Shutdown
		<-spinnerStarted
		return nil
	}

	mChatter.ShutdownFn = func(ctx context.Context) error {
		tracker.record("Chatter.Shutdown")
		return nil
	}

	// Execute high-level Run function to cover it
	err := session.Run(context.Background(), params)
	require.NoError(t, err)

	expectedSequence := []string{
		"Capturer.IsTTY",         // Initial check in Run
		"HistoryRenderer.Render", // Rendering history because LastN > 0
		"AgentFactory",           // Creating the agent
		"Chatter.Subscribe",      // Connect TurnsLogger events
		"Capturer.IsTTY",         // Check in setupUIRendering
		"UIRenderer.SetUseColor", // Config UI
		"Chatter.Subscribe",      // Connect UI events
		"Chatter.SetLimits",      // Apply constraints
		"Chatter.Chat",           // Start conversation
		"UIRenderer.StartSpinnerWithStatus",
		"UIRenderer.StopSpinner", // [REFACTOR] Stop Consumer first because it finishes when Chat returns
		"Chatter.Shutdown",       // Stop Producers last
	}

	assert.Equal(t, expectedSequence, tracker.sequence, "SessionManager must follow exact coordination sequence")
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
	require.Nil(t, deps.GetHistoryManager())
	require.Nil(t, deps.GetLogger())
}

func TestSessionManager_AgentFactory_Error(t *testing.T) {
	t.Parallel()
	// Create an session.SessionManagerInternal with a failing factory
	o := &session.SessionManagerInternal{
		AgentFactory: func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
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
	mCapturer.IsTTYFn = func(v any) bool { return true }

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
			mHistoryRenderer := new(agenttest.MockHistoryRenderer)
			mUIRenderer := new(agenttest.MockUIRenderer)
			orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer)

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

	factory := func(mChatter ports.Chatter) func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
			return mChatter, nil
		}
	}

	setupParams := func(mHistory ports.HistoryManager, mChatter ports.Chatter, mHistoryRenderer *agenttest.MockHistoryRenderer, mUIRenderer *agenttest.MockUIRenderer, mCapturer *agenttest.MockCapturer, mEventBus events.EventBus) session.RunParams {
		return session.RunParams{
			HomeDir:         "home",
			Version:         "1.0.0",
			Stdout:          io.Discard,
			Stderr:          io.Discard,
			AgentFactory:    factory(mChatter),
			HistoryRenderer: mHistoryRenderer,
			UIRenderer:      mUIRenderer,
			Deps:            &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)},
			Capturer:        mCapturer,
			Config: &config.Config{
				Model: "model",
				Mode:  "mode",
			},
		}
	}

	t.Run("Rollback only (no prompt)", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(agenttest.MockHistoryRenderer)
		mUIRenderer := new(agenttest.MockUIRenderer)
		mCapturer := new(agenttest.MockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		eventstest.CleanupBus(t, mEventBus)

		mHistory := new(agenttest.MockHistoryManager)
		mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
		mChatter := new(agenttest.MockChatter)
		// Chat is not expected to be called, but if it is, succeed.
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = ""

		mCapturer.IsTTYFn = func(v any) bool { return true }

		err := session.Run(context.Background(), p)
		assert.NoError(t, err)
		assert.Equal(t, 2, mHistory.GetTotalEntries()) // 1 turn removed (2 messages)
		chatCalls, _, _, _, _ := mChatter.Snapshot()
		if chatCalls != 0 {
			t.Errorf("expected Chat not to be called, got %d calls", chatCalls)
		}
	})

	t.Run("Rollback and Chat", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(agenttest.MockHistoryRenderer)
		mUIRenderer := new(agenttest.MockUIRenderer)
		mCapturer := new(agenttest.MockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		eventstest.CleanupBus(t, mEventBus)

		mHistory := new(agenttest.MockHistoryManager)
		mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
		mChatter := new(agenttest.MockChatter)
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = "hello"

		mCapturer.IsTTYFn = func(v any) bool { return true }
		mUIRenderer.SetUseColorFn = func(use bool) {}
		mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
		mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }
		mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error { return nil }
		mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

		err := session.Run(context.Background(), p)
		assert.NoError(t, err)
	})

	t.Run("Rollback aborts on error", func(t *testing.T) {
		t.Parallel()
		mHistoryRenderer := new(agenttest.MockHistoryRenderer)
		mUIRenderer := new(agenttest.MockUIRenderer)
		mCapturer := new(agenttest.MockCapturer)
		mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		eventstest.CleanupBus(t, mEventBus)

		mHistory := new(agenttest.MockHistoryManager)
		mHistory.SetInternalContents(make([]*llm.Content, 4)) // 2 turns
		mHistory.SetRollbackErr(fmt.Errorf("rollback failed"))
		mChatter := new(agenttest.MockChatter)
		p := setupParams(mHistory, mChatter, mHistoryRenderer, mUIRenderer, mCapturer, mEventBus)
		p.BackN = 1
		p.Prompt = "hello"

		mCapturer.IsTTYFn = func(v any) bool { return true }

		err := session.Run(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rollback failed")

		// Verify that Chatter.Chat was NOT called
		chatCalls, _, _, _, _ := mChatter.Snapshot()
		if chatCalls != 0 {
			t.Errorf("expected Chat not to be called, got %d calls", chatCalls)
		}
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
			name:          "Context Canceled",
			chatErr:       context.Canceled,
			expectedError: context.Canceled.Error(),
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
			eventstest.CleanupBus(t, mEventBus)

			factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
				return mChatter, nil
			}

			mHistoryRenderer := new(agenttest.MockHistoryRenderer)
			mUIRenderer := new(agenttest.MockUIRenderer)
			orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

			sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
				Model: "model",
				Mode:  "mode",
			})
			deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

			mCapturer.IsTTYFn = func(v any) bool { return true }
			mUIRenderer.SetUseColorFn = func(use bool) {}

			spinnerStarted := make(chan struct{})
			spinnerStopped := make(chan struct{})
			mUIRenderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
				if status == " Thinking..." {
					close(spinnerStarted)
					return func() { close(spinnerStopped) }
				}
				return func() {}
			}

			mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {
				// Simulate spinner start before chat fails
				sub(context.Background(), events.InferenceStartedEvent{})
			}

			mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }
			mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error {
				// Wait for the spinner to start before failing chat to avoid racing with fast-drain
				select {
				case <-spinnerStarted:
				case <-time.After(2 * time.Second):
				}
				return tt.chatErr
			}
			mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

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
		})
	}
}

func TestSessionManager_Run_ShutdownError(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	var stderrBuf bytes.Buffer

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, &stderrBuf, factory, mHistoryRenderer, mUIRenderer)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }
	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error { return nil } // Chat succeeds
	mChatter.ShutdownFn = func(ctx context.Context) error { return fmt.Errorf("shutdown timeout") }   // Shutdown fails

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "agent shutdown failed")
	require.Contains(t, err.Error(), "shutdown timeout")
	require.Contains(t, stderrBuf.String(), "Warning: Agent shutdown failed: shutdown timeout")
}

func TestSessionManager_Run_ApplyConfigError(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer)

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return fmt.Errorf("limits error") }
	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to apply configuration")
	require.Contains(t, err.Error(), "limits error")

	// Chat must never be called — Run returns early before the agent loop
	chatCalls, _, _, _, _ := mChatter.Snapshot()
	if chatCalls != 0 {
		t.Errorf("expected Chat not to be called, got %d calls", chatCalls)
	}
	// Shutdown must still be called — the defer always runs
	_, _, _, shutdownCalls, _ := mChatter.Snapshot()
	if shutdownCalls != 1 {
		t.Errorf("expected Shutdown to be called once, got %d", shutdownCalls)
	}
}

func TestSessionManager_SessionID_Fallback(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	mClock := &agenttest.MockClock{}
	mClock.SetCurrentTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mEntropy := &agenttest.MockEntropySource{
		ReadFunc: func(p []byte) (n int, err error) {
			return 0, fmt.Errorf("entropy failure")
		},
	}

	expectedSessionID := fmt.Sprintf("session-%d", mClock.CurrentTime().UnixNano())

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, session.WithClock(mClock), session.WithEntropySource(mEntropy))

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }

	// Exact match on Session ID
	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error {
		if s.ID != expectedSessionID {
			t.Errorf("expected session ID %q, got %q", expectedSessionID, s.ID)
		}
		return nil
	}

	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	now, _ := mClock.Snapshot()
	if now < 1 {
		t.Errorf("expected Now() to be called at least once, got %d", now)
	}
}

func TestSessionManager_SessionID_DeterministicEntropy(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	mClock := &agenttest.MockClock{}

	fixedEntropy := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	mEntropy := &agenttest.MockEntropySource{
		ReadFunc: func(p []byte) (n int, err error) {
			copy(p, fixedEntropy)
			return len(fixedEntropy), nil
		},
	}

	expectedSessionID := "session-0102030405060708"

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, session.WithClock(mClock), session.WithEntropySource(mEntropy))

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }

	// Exact match on Session ID
	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error {
		if s.ID != expectedSessionID {
			t.Errorf("expected session ID %q, got %q", expectedSessionID, s.ID)
		}
		return nil
	}

	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)
}

func TestSessionManager_SessionID_ShortRead_Fallback(t *testing.T) {
	t.Parallel()
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	mClock := &agenttest.MockClock{}
	mClock.SetCurrentTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	// Entropy source returns a short read (e.g., only 4 bytes instead of 8)
	shortEntropy := []byte{0x01, 0x02, 0x03, 0x04}
	var readCount int
	mEntropy := &agenttest.MockEntropySource{
		ReadFunc: func(p []byte) (n int, err error) {
			readCount++
			if readCount == 1 {
				copy(p, shortEntropy)
				return len(shortEntropy), nil
			}
			return 0, io.EOF
		},
	}

	// Since it's a short read, it should fallback to timestamp-based ID
	expectedSessionID := fmt.Sprintf("session-%d", mClock.CurrentTime().UnixNano())

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, io.Discard, factory, mHistoryRenderer, mUIRenderer, session.WithClock(mClock), session.WithEntropySource(mEntropy))

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }

	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error {
		if s.ID != expectedSessionID {
			t.Errorf("expected session ID %q, got %q", expectedSessionID, s.ID)
		}
		return nil
	}

	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	now, _ := mClock.Snapshot()
	if now < 1 {
		t.Errorf("expected Now() to be called at least once, got %d", now)
	}
}

func TestSessionManager_SetupUIRendering_HandleEventError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		prepareBridge   func(t *testing.T, bridge *ui.Bridge) context.Context
		expectedMethod  string // "Warn" or "Debug"
		expectedMessage string
	}{
		{
			name: "actor dead",
			prepareBridge: func(t *testing.T, bridge *ui.Bridge) context.Context {
				t.Helper()
				// Saturate the event queue (default capacity = 100) so the next critical
				// event is forced into the backpressure path where loopCtx.Done() fires.
				for i := 0; i < 100; i++ {
					_ = bridge.HandleEvent(context.Background(), events.TurnStatusEvent{
						Status: events.TurnStatus{SessionTurns: i},
					})
				}
				// Kill the bridge's actor loop so HandleEvent returns ErrActorDead.
				bridge.KillActor()
				return context.Background()
			},
			expectedMethod:  "Warn",
			expectedMessage: "Bridge event failed: actor is dead",
		},
		{
			name: "context canceled",
			prepareBridge: func(t *testing.T, bridge *ui.Bridge) context.Context {
				t.Helper()
				// Pass a cancelled context so HandleEvent returns a pure context.Canceled
				// (not wrapped with ErrActorDead).
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			expectedMethod:  "Debug",
			expectedMessage: "Bridge event skipped: context cancelled",
		},
		{
			name: "generic error",
			prepareBridge: func(t *testing.T, bridge *ui.Bridge) context.Context {
				t.Helper()
				// Pass a deadline-exceeded context so HandleEvent returns an error
				// that is neither ErrActorDead nor context.Canceled.
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
				// Keep the cancel function alive so the context stays in DeadlineExceeded
				// state; it is a no-op on an already-expired deadline.
				_ = cancel
				return ctx
			},
			expectedMethod:  "Warn",
			expectedMessage: "Failed to handle bridge event",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockLogger := new(testfixtures.SpyLogger)

			mChatter := new(agenttest.MockChatter)
			var uiSub func(context.Context, events.Event)
			mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {
				uiSub = sub
			}
			mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }

			mUIRenderer := new(agenttest.MockUIRenderer)
			mUIRenderer.SetUseColorFn = func(use bool) {}

			mCapturer := new(agenttest.MockCapturer)
			mCapturer.IsTTYFn = func(v any) bool { return true }

			mHistoryRenderer := new(agenttest.MockHistoryRenderer)
			orch := session.NewSessionManager("home", "1.0.0", nil,
				io.Discard, io.Discard, nil, mHistoryRenderer, mUIRenderer)

			sCfg := &session.SessionConfigInternal{
				Config: &config.Config{},
			}
			deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, Logger: mockLogger, TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

			bridge, err := session.AsSessionManagerInternal(orch).ApplyConfiguration(
				context.Background(), mChatter, sCfg, deps, mCapturer)
			require.NoError(t, err)
			require.NotNil(t, uiSub, "Subscribe callback must be captured")

			ctx := tt.prepareBridge(t, bridge)

			// Fire a critical event through the captured subscription callback.
			uiSub(ctx, events.TurnStatusEvent{
				Status: events.TurnStatus{SessionTurns: 1},
			})

			switch tt.expectedMethod {
			case "Warn":
				require.True(t, mockLogger.CalledWith("Warn", tt.expectedMessage),
					"expected logger.Warn to be called with %q", tt.expectedMessage)
			case "Debug":
				require.True(t, mockLogger.CalledWith("Debug", tt.expectedMessage),
					"expected logger.Debug to be called with %q", tt.expectedMessage)
			}

			// Cleanup bridge — Listen() was never started, so AbortStart is needed.
			bridge.AbortStart()
			bridge.CloseInput()
			bridge.Cleanup()
		})
	}
}
