// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockChatter struct {
	mock.Mock
}

func (m *mockChatter) Chat(ctx context.Context, s *Session, prompt string) error {
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

func (m *mockChatter) GetCostTracker() domain_pricing.ICostTracker {
	args := m.Called()
	return args.Get(0).(domain_pricing.ICostTracker)
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

type mockCapturer struct {
	mock.Mock
}

func (m *mockCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

// --- Tests ---

func TestSessionDependencies_Structure(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SessionDependencies{
		Paths: &persistence.Paths{
			ModeDir:         tmpDir,
			LogPath:         filepath.Join(tmpDir, "tokens.log"),
			CommandsLogPath: filepath.Join(tmpDir, "commands.log"),
		},
	}
	require.NotNil(t, deps.Paths)
}

func TestSessionConfig_Structure(t *testing.T) {
	cfg := &SessionConfig{
		Config: &config.Config{
			Model: "test-model",
		},
	}
	require.Equal(t, "test-model", cfg.Config.Model)
}

func TestOrchestrator_Run_Success(t *testing.T) {
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus()

	agentFactory := func(client llm.LLMGateway, hManager services.HistoryManager, registry tools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) Chatter {
		return mChatter
	}

	orch := NewOrchestrator("home", "1.0.0", nil, io.Discard, io.Discard, agentFactory)

	sCfg := &SessionConfig{
		Prompt: "hello",
		Config: &config.Config{
			Model: "model",
			Mode:  "mode",
		},
	}
	deps := &SessionDependencies{
		HistoryManager: mHistory,
		EventBus:       mEventBus,
		Paths:          &persistence.Paths{},
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)
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
		name     string
		event    events.Event
		setup    func()
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
	mEventBus := events.NewSimpleEventBus()

	agentFactory := func(client llm.LLMGateway, hManager services.HistoryManager, registry tools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) Chatter {
		return mChatter
	}

	orch := NewOrchestrator("home", "1.0.0", nil, io.Discard, io.Discard, agentFactory)

	sCfg := &SessionConfig{
		Prompt: "hello",
		Config: &config.Config{
			Model: "model",
			Mode:  "mode",
		},
	}
	deps := &SessionDependencies{
		HistoryManager: mHistory,
		EventBus:       mEventBus,
		Paths:          &persistence.Paths{},
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)
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
	mEventBus := events.NewSimpleEventBus()

	agentFactory := func(client llm.LLMGateway, hManager services.HistoryManager, registry tools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) Chatter {
		return nil
	}

	orch := NewOrchestrator("home", "1.0.0", nil, io.Discard, io.Discard, agentFactory)

	sCfg := &SessionConfig{
		Prompt: "",
		LastN:  5,
		Config: &config.Config{},
	}
	deps := &SessionDependencies{
		HistoryManager: mHistory,
		EventBus:       mEventBus,
		Paths:          &persistence.Paths{},
	}

	mCapturer.On("IsTTY", io.Discard).Return(true)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	mCapturer.AssertExpectations(t)
}

func TestOrchestrator_ApplyConfiguration_Error(t *testing.T) {
	orch := NewOrchestrator("home", "1.0.0", nil, io.Discard, io.Discard, nil)
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	
	sCfg := &SessionConfig{
		Config: &config.Config{
			MaxToolTurns: 10,
		},
	}
	paths := &persistence.Paths{}
	pData := domain_pricing.PricingData{}

	mCapturer.On("IsTTY", mock.Anything).Return(true)
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, 10, mock.Anything, mock.Anything).Return(fmt.Errorf("limits error"))

	err := orch.applyConfiguration(context.Background(), mChatter, sCfg, paths, pData, mCapturer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limits error")
}

func TestUIBridge_RelayStream_ContextCancelledDuringSend(t *testing.T) {
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer, true, true, false, true, "log.txt")

	ctx, cancel := context.WithCancel(context.Background())
	stream := make(chan *llm.Content)
	uiCh := make(chan *llm.Content) // Unbuffered

	go func() {
		stream <- &llm.Content{}
	}()

	// Wait a bit to ensure relayStream is blocked on sending to uiCh
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	bridge.relayStream(ctx, stream, uiCh)
	// Should return when context is cancelled
}
