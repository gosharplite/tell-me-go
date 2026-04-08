// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
func (s *stubUIRenderer) SetUseColor(use bool)       {}
func (s *stubUIRenderer) SetForceSpinner(force bool) {}

// stubHistoryRenderer is a stub implementation of ports.HistoryRenderer for testing.
type stubHistoryRenderer struct{}

func (s *stubHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
}

// stubHistoryBrowser is a stub implementation of ports.HistoryBrowser for testing.
type stubHistoryBrowser struct{}

func (s *stubHistoryBrowser) Browse(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	return nil
}

// mockSessionLifecycleManager is a mock of SessionLifecycleManager.
type mockSessionLifecycleManager struct {
	mock.Mock
}

func (m *mockSessionLifecycleManager) BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(context.Context) error, error) {
	args := m.Called(ctx, cfg, configPath, newSession, capturer)
	var deps ports.SessionDependencies
	if args.Get(0) != nil {
		deps = args.Get(0).(ports.SessionDependencies)
	}
	var hManager ports.HistoryManager
	if args.Get(1) != nil {
		hManager = args.Get(1).(ports.HistoryManager)
	}
	return deps, hManager, args.Get(2).(func(context.Context) error), args.Error(3)
}

func (m *mockSessionLifecycleManager) FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
	args := m.Called(ctx, hManager, deps, cfg)
	return args.Error(0)
}

// mockServiceSecurityManager is a mock of Manager.
type mockServiceSecurityManager struct {
	mock.Mock
}

func (m *mockServiceSecurityManager) IsPathSafe(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) IsPathWritable(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	args := m.Called(ctx, label, detail, reason, isSafe)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceSecurityManager) LogAudit(action string, args ...any) {
	m.Called(action, args)
}
func (m *mockServiceSecurityManager) TerminalLock()         { m.Called() }
func (m *mockServiceSecurityManager) TerminalUnlock()       { m.Called() }
func (m *mockServiceSecurityManager) Prompt(message string) { m.Called(message) }
func (m *mockServiceSecurityManager) Warn(message string)   { m.Called(message) }
func (m *mockServiceSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceSecurityManager) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) IsCommandAllowed(command string) bool {
	return m.Called(command).Bool(0)
}
func (m *mockServiceSecurityManager) IsBypassActive() bool { return m.Called().Bool(0) }
func (m *mockServiceSecurityManager) Close() error         { return m.Called().Error(0) }

// mockServiceSessionDependencies is a mock of SessionDependencies.
type mockServiceSessionDependencies struct {
	mock.Mock
}

func (m *mockServiceSessionDependencies) GetGateway() llm.LLMGateway { return nil }
func (m *mockServiceSessionDependencies) GetHistoryManager() ports.HistoryManager {
	return m.Called().Get(0).(ports.HistoryManager)
}
func (m *mockServiceSessionDependencies) GetRegistry() tools.Registry { return nil }
func (m *mockServiceSessionDependencies) GetSecurityManager() security.Manager {
	return m.Called().Get(0).(security.Manager)
}
func (m *mockServiceSessionDependencies) GetEventBus() events.EventBus {
	return m.Called().Get(0).(events.EventBus)
}
func (m *mockServiceSessionDependencies) GetLogger() *slog.Logger {
	return m.Called().Get(0).(*slog.Logger)
}
func (m *mockServiceSessionDependencies) GetTurnsLogger() ports.TurnsLogger {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.TurnsLogger)
}
func (m *mockServiceSessionDependencies) GetPaths() *persistence.Paths {
	return m.Called().Get(0).(*persistence.Paths)
}
func (m *mockServiceSessionDependencies) GetPricingOverrides() map[string]pricing.ModelPricing {
	return nil
}
func (m *mockServiceSessionDependencies) GetTracker() pricing.CostTracker { return nil }
func (m *mockServiceSessionDependencies) GetPricingData() pricing.PricingData {
	return pricing.PricingData{}
}
func (m *mockServiceSessionDependencies) GetClient() llm.LLMClient { return nil }
func (m *mockServiceSessionDependencies) GetSessionProvider() ports.SessionProvider {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.SessionProvider)
}

// mockServiceEventBus is a mock of EventBus.
type mockServiceEventBus struct {
	mock.Mock
}

func (m *mockServiceEventBus) Publish(ctx context.Context, e events.Event) error {
	return m.Called(ctx, e).Error(0)
}
func (m *mockServiceEventBus) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}
func (m *mockServiceEventBus) Shutdown(ctx context.Context) error { return m.Called(ctx).Error(0) }
func (m *mockServiceEventBus) Flush(ctx context.Context) error    { return m.Called(ctx).Error(0) }

// mockServiceAgent is a mock of Chatter.
type mockServiceAgent struct {
	mock.Mock
}

func (m *mockServiceAgent) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return m.Called(ctx, sess, prompt).Error(0)
}
func (m *mockServiceAgent) SetLimits(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
	return m.Called(ctx, maxTurns, contextWindow, historyTurns).Error(0)
}
func (m *mockServiceAgent) SetTieredThreshold(ctx context.Context, threshold int) error {
	return m.Called(ctx, threshold).Error(0)
}
func (m *mockServiceAgent) Subscribe(handler func(context.Context, events.Event)) { m.Called(handler) }
func (m *mockServiceAgent) Shutdown(ctx context.Context) error                    { return m.Called(ctx).Error(0) }

// mockServiceCapturer is a mock of Capturer.
type mockServiceCapturer struct {
	mock.Mock
}

func (m *mockServiceCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *mockServiceCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	callArgs := m.Called(ctx, args, opts)
	return callArgs.String(0), callArgs.Error(1)
}

func (m *mockServiceCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceCapturer) Warn(msg string)   { m.Called(msg) }
func (m *mockServiceCapturer) Prompt(msg string) { m.Called(msg) }
func (m *mockServiceCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceCapturer) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

type mockTurnsLogger struct {
	mock.Mock
}

func (m *mockTurnsLogger) HandleEvent(ctx context.Context, e events.Event) {
	m.Called(ctx, e)
}

func (m *mockTurnsLogger) Close() error {
	return m.Called().Error(0)
}

func TestProcessMessage(t *testing.T) {
	errBuild := errors.New("build error")

	tests := []struct {
		name        string
		setupMock   func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error
		cmd         ChatCommand
		cfg         *config.Config
		wantErr     bool
		errMsg      string
		expectedErr error
	}{
		{
			name: "Success",
			cmd: ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{
					Mode: "assistant",
					Providers: map[string]config.LLMProvider{
						"test": {Model: "test-model"},
					},
					SelectedProvider: "test",
				}
				cleanupCalled := false
				cleanup := func(context.Context) error {
					cleanupCalled = true
					return tl.Close()
				}

				mockHM := &mockHistoryManagerForRetry{}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				sf.On("FinalizeSession", mock.Anything, mock.Anything, deps, cfg).Return(nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetPaths").Return(&persistence.Paths{TurnsLogPath: "turns.log"})
				deps.On("GetHistoryManager").Return(mockHM)
				deps.On("GetPricingData").Return(pricing.PricingData{})
				deps.On("GetLogger").Return(slog.Default())
				deps.On("GetTurnsLogger").Return(tl)
				deps.On("GetSessionProvider").Return(nil)

				tl.On("Close").Return(nil)
				bus.On("Shutdown", mock.Anything).Return(nil)

				agent.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agent.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
				agent.On("Subscribe", mock.Anything).Return()
				agent.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
				agent.On("Shutdown", mock.Anything).Return(nil)

				cap.On("IsTTY", mock.Anything).Return(true)
				cap.On("Close", mock.Anything).Return(nil)

				return func(context.Context) error {
					assert.True(t, cleanupCalled)
					return nil
				}
			},
		},
		{
			name: "BuildSessionDepsError",
			cmd: ChatCommand{ConfigPath: "config.yaml"},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{Mode: "assistant"}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(nil, nil, func(context.Context) error { return nil }, errBuild)
				return nil
			},
			wantErr:     true,
			errMsg:      "build error",
			expectedErr: errBuild,
		},
		{
			name: "RetrySuccess",
			cmd: ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{
					Mode: "assistant",
					Providers: map[string]config.LLMProvider{
						"test": {Model: "test-model"},
					},
					SelectedProvider: "test",
				}
				cleanupCalled := false
				cleanup := func(context.Context) error {
					cleanupCalled = true
					return tl.Close()
				}

				mockHM := &mockHistoryManagerForRetry{msg: "retry this", turns: 2}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				sf.On("FinalizeSession", mock.Anything, mock.Anything, deps, cfg).Return(nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetPaths").Return(&persistence.Paths{TurnsLogPath: "turns.log"})
				deps.On("GetHistoryManager").Return(mockHM)
				deps.On("GetPricingData").Return(pricing.PricingData{})
				deps.On("GetLogger").Return(slog.Default())
				deps.On("GetTurnsLogger").Return(tl)
				deps.On("GetSessionProvider").Return(nil)

				tl.On("Close").Return(nil)
				bus.On("Shutdown", mock.Anything).Return(nil)

				agent.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agent.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
				agent.On("Subscribe", mock.Anything).Return()
				agent.On("Chat", mock.Anything, mock.Anything, "retry this").Return(nil)
				agent.On("Shutdown", mock.Anything).Return(nil)

				cap.On("Confirm", mock.Anything, mock.MatchedBy(func(s string) bool {
					return strings.Contains(s, "retry this")
				})).Return(true, nil)
				cap.On("IsTTY", mock.Anything).Return(true)
				cap.On("Close", mock.Anything).Return(nil)

				return func(context.Context) error {
					assert.True(t, cleanupCalled)
					return nil
				}
			},
		},
		{
			name: "RetryAborted",
			cmd: ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{
					Mode: "assistant",
					Providers: map[string]config.LLMProvider{
						"test": {Model: "test-model"},
					},
					SelectedProvider: "test",
				}
				cleanup := func(context.Context) error { return nil }

				mockHM := &mockHistoryManagerForRetry{msg: "retry this", turns: 2}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				bus.On("Shutdown", mock.Anything).Return(nil)

				cap.On("Confirm", mock.Anything, mock.Anything).Return(false, nil)

				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sf := &mockSessionLifecycleManager{}
			sm := &mockServiceSecurityManager{}
			capturer := &mockServiceCapturer{}
			deps := &mockServiceSessionDependencies{}
			bus := &mockServiceEventBus{}
			agent := &mockServiceAgent{}
			tl := &mockTurnsLogger{}

			chatterFactory := ports.ChatterFactory(func(ctx context.Context, sd ports.SessionDependencies, cCfg ports.ChatterConfig) (ports.Chatter, error) {
				return agent, nil
			})

			service := NewChatService(
				"home", "v1", io.Discard, io.Discard, sm,
				sf, chatterFactory, &stubUIRenderer{}, &stubHistoryRenderer{}, &stubHistoryBrowser{}, nil,
			)

			var verify func(context.Context) error
			if tt.setupMock != nil {
				verify = tt.setupMock(sf, sm, capturer, deps, bus, agent, tl)
			}

			err := service.ProcessMessage(ctx, tt.cfg, tt.cmd, capturer)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				if tt.expectedErr != nil {
					assert.ErrorIs(t, err, tt.expectedErr)
				}
			} else {
				assert.NoError(t, err)
			}

			if verify != nil {
				_ = verify(context.Background())
			}

			sf.AssertExpectations(t)
			if !tt.wantErr {
				if tt.name != "RetryAborted" {
					agent.AssertExpectations(t)
					tl.AssertExpectations(t)
				}
				bus.AssertExpectations(t)
			}
		})
	}
}

func TestGetLastUserMessage(t *testing.T) {
	ctx := context.Background()
	sm := &mockServiceSecurityManager{}

	mockHM := &mockHistoryManagerForRetry{msg: "last message", turns: 1}

	service := NewChatService(
		"home", "v1", io.Discard, io.Discard, sm,
		nil, nil, &stubUIRenderer{}, &stubHistoryRenderer{}, &stubHistoryBrowser{}, nil,
	)

	msg, turns, err := service.GetLastUserMessage(ctx, mockHM)

	assert.NoError(t, err)
	assert.Equal(t, "last message", msg)
	assert.Equal(t, 1, turns)
}

type mockHistoryManagerForRetry struct {
	msg   string
	turns int
	err   error
}

func (m *mockHistoryManagerForRetry) GetTotalEntries() int { return 0 }
func (m *mockHistoryManagerForRetry) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return nil, nil
}
func (m *mockHistoryManagerForRetry) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return m.msg, m.turns, m.err
}
func (m *mockHistoryManagerForRetry) GetResolver() llm.AssetResolver { return nil }
func (m *mockHistoryManagerForRetry) SetContents(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockHistoryManagerForRetry) AddContent(ctx context.Context, content *llm.Content) error {
	return nil
}
func (m *mockHistoryManagerForRetry) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	return nil
}
func (m *mockHistoryManagerForRetry) Save(ctx context.Context) error { return nil }
func (m *mockHistoryManagerForRetry) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockHistoryManagerForRetry) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
}
func (m *mockHistoryManagerForRetry) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

func (m *mockHistoryManagerForRetry) GetFilePath() string { return "" }

type mockFileSystemStream struct {
	persistence.FileSystem
	mock.Mock
}

func (m *mockFileSystemStream) Open(ctx context.Context, name string) (persistence.File, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(persistence.File), args.Error(1)
}

type minimalFile struct {
	io.Reader
}

func (f *minimalFile) Close() error                                  { return nil }
func (f *minimalFile) ReadAt(p []byte, off int64) (n int, err error) { return 0, nil }
func (f *minimalFile) ReadDir(n int) ([]os.DirEntry, error)          { return nil, nil }
func (f *minimalFile) Seek(offset int64, whence int) (int64, error)  { return 0, nil }
func (f *minimalFile) Write(p []byte) (int, error)                   { return 0, nil }

type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestStreamTurnsLog(t *testing.T) {
	ctx := context.Background()
	homeDir := "/home/user"

	tests := []struct {
		name        string
		mode        string
		setupMock   func(mFS *mockFileSystemStream)
		expectedOut string
		wantErr     bool
	}{
		{
			name: "Success",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				logPath := persistence.ResolvePaths(homeDir, "assistant").TurnsLogPath
				mFS.On("Open", mock.Anything, logPath).Return(&minimalFile{Reader: strings.NewReader("turn 1: hello")}, nil)
			},
			expectedOut: "turn 1: hello",
			wantErr:     false,
		},
		{
			name: "LogFileMissing",
			mode: "developer",
			setupMock: func(mFS *mockFileSystemStream) {
				logPath := persistence.ResolvePaths(homeDir, "developer").TurnsLogPath
				mFS.On("Open", mock.Anything, logPath).Return(nil, os.ErrNotExist)
			},
			expectedOut: "No turns log found for this session yet.\n",
			wantErr:     false,
		},
		{
			name: "PermissionDenied",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				logPath := persistence.ResolvePaths(homeDir, "assistant").TurnsLogPath
				mFS.On("Open", mock.Anything, logPath).Return(nil, os.ErrPermission)
			},
			wantErr: true,
		},
		{
			name: "ReadError",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				logPath := persistence.ResolvePaths(homeDir, "assistant").TurnsLogPath
				mFS.On("Open", mock.Anything, logPath).Return(&minimalFile{Reader: &errorReader{}}, nil)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mFS := new(mockFileSystemStream)
			if tt.setupMock != nil {
				tt.setupMock(mFS)
			}

			service := NewChatService(
				homeDir, "v1", io.Discard, io.Discard, nil,
				nil, nil, nil, nil, nil, mFS,
			)

			var out bytes.Buffer
			err := service.StreamTurnsLog(ctx, &config.Config{Mode: tt.mode}, &out)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedOut, out.String())
			}
			mFS.AssertExpectations(t)
		})
	}
}
