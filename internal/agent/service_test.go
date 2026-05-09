// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agentinternal"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestProcessMessage(t *testing.T) {
	errBuild := errors.New("build error")
	errChat := errors.New("chat error")
	errFinalize := errors.New("finalize error")

	tests := []struct {
		name             string
		setupMock        func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error
		cmd              agent.ChatCommand
		cfg              *config.Config
		wantErr          bool
		errMsg           string
		expectedErr      error
		extraExpectedErr error
	}{
		{
			name: "Success",
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
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
				deps.On("GetLogger").Return(&ports.NoOpLogger{})
				deps.On("GetTurnsLogger").Return(tl)
				deps.On("GetSessionProvider").Return(nil)

				tl.On("Close").Return(nil)
				bus.On("Shutdown", mock.Anything).Return(nil)

				agentMock.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agentMock.On("Subscribe", mock.Anything).Return()
				agentMock.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
				agentMock.On("Shutdown", mock.Anything).Return(nil)

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
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml"},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
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
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
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
				deps.On("GetLogger").Return(&ports.NoOpLogger{})
				deps.On("GetTurnsLogger").Return(tl)
				deps.On("GetSessionProvider").Return(nil)

				tl.On("Close").Return(nil)
				bus.On("Shutdown", mock.Anything).Return(nil)

				agentMock.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agentMock.On("Subscribe", mock.Anything).Return()
				agentMock.On("Chat", mock.Anything, mock.Anything, "retry this").Return(nil)
				agentMock.On("Shutdown", mock.Anything).Return(nil)

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
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
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
		{
			name: "RetryNoHistory",
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				mockHM := &mockHistoryManagerForRetry{msg: "", turns: 0}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				deps.On("GetEventBus").Return(bus)
				bus.On("Shutdown", mock.Anything).Return(nil)
				return nil
			},
			wantErr: true,
			errMsg:  "no previous user message found to retry",
		},
		{
			name: "RetryHistoryError",
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				mockHM := &mockHistoryManagerForRetry{err: errors.New("db error")}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				deps.On("GetEventBus").Return(bus)
				bus.On("Shutdown", mock.Anything).Return(nil)
				return nil
			},
			wantErr: true,
			errMsg:  "failed to get last user message for retry",
		},
		{
			name: "RetryConfirmError",
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				mockHM := &mockHistoryManagerForRetry{msg: "retry me", turns: 1}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				deps.On("GetEventBus").Return(bus)
				bus.On("Shutdown", mock.Anything).Return(nil)
				cap.On("Confirm", mock.Anything, mock.Anything).Return(false, errors.New("UI error"))
				return nil
			},
			wantErr: true,
			errMsg:  "UI error",
		},
		{
			name: "FinalizeErrorOnly",
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{
					Mode: "assistant",
					Providers: map[string]config.LLMProvider{
						"test": {Model: "test-model"},
					},
					SelectedProvider: "test",
				}
				cleanup := func(context.Context) error { return nil }
				mockHM := &mockHistoryManagerForRetry{}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				sf.On("FinalizeSession", mock.Anything, mock.Anything, deps, cfg).Return(errFinalize)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetPaths").Return(&persistence.Paths{TurnsLogPath: "turns.log"})
				deps.On("GetHistoryManager").Return(mockHM)
				deps.On("GetPricingData").Return(pricing.PricingData{})
				deps.On("GetLogger").Return(&ports.NoOpLogger{})
				deps.On("GetTurnsLogger").Return(tl)
				deps.On("GetSessionProvider").Return(nil)

				bus.On("Shutdown", mock.Anything).Return(nil)

				agentMock.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agentMock.On("Subscribe", mock.Anything).Return()
				agentMock.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
				agentMock.On("Shutdown", mock.Anything).Return(nil)

				cap.On("IsTTY", mock.Anything).Return(true)

				return nil
			},
			wantErr:     true,
			errMsg:      "",
			expectedErr: errFinalize,
		},
		{
			name: "DoubleError",
			cmd:  agent.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
			cfg: &config.Config{
				Mode: "assistant",
				Providers: map[string]config.LLMProvider{
					"test": {Model: "test-model"},
				},
				SelectedProvider: "test",
			},
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, sm *agenttest.MockServiceSecurityManager, cap *agenttest.MockServiceCapturer, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, agentMock *agenttest.MockServiceAgent, tl *agenttest.MockTurnsLogger) func(context.Context) error {
				cfg := &config.Config{
					Mode: "assistant",
					Providers: map[string]config.LLMProvider{
						"test": {Model: "test-model"},
					},
					SelectedProvider: "test",
				}
				cleanup := func(context.Context) error { return nil }
				mockHM := &mockHistoryManagerForRetry{}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, mockHM, cleanup, nil)
				sf.On("FinalizeSession", mock.Anything, mock.Anything, deps, cfg).Return(errFinalize)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetPaths").Return(&persistence.Paths{TurnsLogPath: "turns.log"})
				deps.On("GetHistoryManager").Return(mockHM)
				deps.On("GetPricingData").Return(pricing.PricingData{})
				deps.On("GetLogger").Return(&ports.NoOpLogger{})
				deps.On("GetTurnsLogger").Return(tl)
				deps.On("GetSessionProvider").Return(nil)

				bus.On("Shutdown", mock.Anything).Return(nil)

				agentMock.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agentMock.On("Subscribe", mock.Anything).Return()
				agentMock.On("Chat", mock.Anything, mock.Anything, "hello").Return(errChat)
				agentMock.On("Shutdown", mock.Anything).Return(nil)

				cap.On("IsTTY", mock.Anything).Return(true)

				return nil
			},
			wantErr:          true,
			errMsg:           "",
			expectedErr:      errChat,
			extraExpectedErr: errFinalize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sf := &agentinternal.MockSessionLifecycleManager{}
			sm := &agenttest.MockServiceSecurityManager{}
			capturer := &agenttest.MockServiceCapturer{}
			deps := &agenttest.MockServiceSessionDependencies{}
			bus := &agenttest.MockServiceEventBus{}
			agentMock := &agenttest.MockServiceAgent{}
			tl := &agenttest.MockTurnsLogger{}

			chatterFactory := ports.ChatterFactory(func(ctx context.Context, sd ports.SessionDependencies, cCfg ports.ChatterConfig) (ports.Chatter, error) {
				return agentMock, nil
			})

			service := agent.NewChatService(
				"home", "v1", io.Discard, io.Discard, sm,
				sf, chatterFactory, &agenttest.StubUIRenderer{}, &agenttest.StubHistoryRenderer{}, &agenttest.StubHistoryBrowser{}, nil,
			)

			var verify func(context.Context) error
			if tt.setupMock != nil {
				verify = tt.setupMock(sf, sm, capturer, deps, bus, agentMock, tl)
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
				if tt.extraExpectedErr != nil {
					assert.ErrorIs(t, err, tt.extraExpectedErr)
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
					agentMock.AssertExpectations(t)
					tl.AssertExpectations(t)
				}
				bus.AssertExpectations(t)
			}
		})
	}
}

func TestGetLastUserMessage(t *testing.T) {
	ctx := context.Background()
	sm := &agenttest.MockServiceSecurityManager{}

	mockHM := &mockHistoryManagerForRetry{msg: "last message", turns: 1}

	service := agent.NewChatService(
		"home", "v1", io.Discard, io.Discard, sm,
		nil, nil, &agenttest.StubUIRenderer{}, &agenttest.StubHistoryRenderer{}, &agenttest.StubHistoryBrowser{}, nil,
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
	closeErr error
}

func (f *minimalFile) Close() error                                  { return f.closeErr }
func (f *minimalFile) ReadAt(p []byte, off int64) (n int, err error) { return 0, nil }
func (f *minimalFile) ReadDir(n int) ([]os.DirEntry, error)          { return nil, nil }
func (f *minimalFile) Seek(offset int64, whence int) (int64, error)  { return 0, nil }
func (f *minimalFile) Write(p []byte) (int, error)                   { return 0, nil }
func (f *minimalFile) Sync() error                                   { return nil }

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
		errMsg      string
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
			errMsg:  "failed to open turns log",
		},
		{
			name: "ReadError",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				logPath := persistence.ResolvePaths(homeDir, "assistant").TurnsLogPath
				mFS.On("Open", mock.Anything, logPath).Return(&minimalFile{Reader: &errorReader{}}, nil)
			},
			wantErr: true,
			errMsg:  "failed to stream log",
		},
		{
			name: "CloseError",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				logPath := persistence.ResolvePaths(homeDir, "assistant").TurnsLogPath
				mFS.On("Open", mock.Anything, logPath).Return(&minimalFile{
					Reader:   strings.NewReader("log content"),
					closeErr: errors.New("close failure"),
				}, nil)
			},
			expectedOut: "log content",
			wantErr:     true,
			errMsg:      "failed to close turns log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mFS := new(mockFileSystemStream)
			if tt.setupMock != nil {
				tt.setupMock(mFS)
			}

			service := agent.NewChatService(
				homeDir, "v1", io.Discard, io.Discard, nil,
				nil, nil, nil, nil, nil, mFS,
			)

			var out bytes.Buffer
			err := service.StreamTurnsLog(ctx, &config.Config{Mode: tt.mode}, &out)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedOut, out.String())
			}
			mFS.AssertExpectations(t)
		})
	}
}

func (m *mockHistoryManagerForRetry) Sync(ctx context.Context) error {
	return nil
}

// mockHealthCheckManager is a mock of ports.HealthCheckManager for RunDiagnostics tests.
type mockHealthCheckManager struct {
	mock.Mock
}

func (m *mockHealthCheckManager) CheckAll(ctx context.Context) (*ports.HealthReport, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ports.HealthReport), args.Error(1)
}

func (m *mockHealthCheckManager) CheckComponent(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error) {
	args := m.Called(ctx, comp)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ports.ComponentReport), args.Error(1)
}

// mockUIRendererForDiag embeds StubUIRenderer and overrides methods needed for RunDiagnostics assertions.
type mockUIRendererForDiag struct {
	agenttest.StubUIRenderer
	mock.Mock
}

func (m *mockUIRendererForDiag) SetUseColor(use bool) {
	m.Called(use)
}

func (m *mockUIRendererForDiag) IsTerminalContext() bool {
	return m.Called().Bool(0)
}

func (m *mockUIRendererForDiag) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {
	m.Called(ctx, report)
}

func TestRunDiagnostics(t *testing.T) {
	errBuild := errors.New("build error")
	errCheck := errors.New("check error")

	healthyReport := &ports.HealthReport{
		OverallStatus: ports.StatusHealthy,
		Components: map[ports.Component]ports.ComponentReport{
			ports.CompPersistence: {Component: ports.CompPersistence, Status: ports.StatusHealthy, Message: "OK"},
		},
	}
	unhealthyReport := &ports.HealthReport{
		OverallStatus: ports.StatusUnhealthy,
		Components: map[ports.Component]ports.ComponentReport{
			ports.CompLLMProvider: {Component: ports.CompLLMProvider, Status: ports.StatusUnhealthy, Message: "unreachable"},
		},
	}

	tests := []struct {
		name       string
		jsonOutput bool
		setupMock  func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag)
		wantErr    bool
		errMsg     string
		checkOut   func(t *testing.T, stdout string)
	}{
		{
			name:       "success UI output",
			jsonOutput: false,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetHealthManager").Return(hcm)
				bus.On("Shutdown", mock.Anything).Return(nil)

				hcm.On("CheckAll", mock.Anything).Return(healthyReport, nil)

				uir.On("IsTerminalContext").Return(false)
				uir.On("SetUseColor", false).Return()
				uir.On("RenderHealthReport", mock.Anything, healthyReport).Return()
			},
		},
		{
			name:       "success JSON output",
			jsonOutput: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetHealthManager").Return(hcm)
				bus.On("Shutdown", mock.Anything).Return(nil)

				hcm.On("CheckAll", mock.Anything).Return(healthyReport, nil)
			},
			checkOut: func(t *testing.T, stdout string) {
				t.Helper()
				assert.Contains(t, stdout, `"overall_status": "healthy"`)
				assert.Contains(t, stdout, `"persistence"`)
			},
		},
		{
			name:       "JSON marshal error",
			jsonOutput: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetHealthManager").Return(hcm)
				bus.On("Shutdown", mock.Anything).Return(nil)

				// Construct a report with an un-marshalable Details field.
				// json.MarshalIndent cannot serialize a channel, so the defensive
				// guard at service.go:258-260 is exercised.
				unmarshalableReport := &ports.HealthReport{
					OverallStatus: ports.StatusHealthy,
					Components: map[ports.Component]ports.ComponentReport{
						ports.CompPersistence: {
							Component: ports.CompPersistence,
							Status:    ports.StatusHealthy,
							Message:   "OK",
							Details:   make(chan int),
						},
					},
				}
				hcm.On("CheckAll", mock.Anything).Return(unmarshalableReport, nil)
			},
			wantErr: true,
			errMsg:  "failed to serialize health report",
		},
		{
			name: "build deps error",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(nil, nil, (func(context.Context) error)(nil), errBuild)
			},
			wantErr: true,
			errMsg:  "build error",
		},
		{
			name: "nil health manager",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetHealthManager").Return(nil)
				bus.On("Shutdown", mock.Anything).Return(nil)
			},
			wantErr: true,
			errMsg:  "health check manager not available",
		},
		{
			name: "CheckAll error",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetHealthManager").Return(hcm)
				bus.On("Shutdown", mock.Anything).Return(nil)

				hcm.On("CheckAll", mock.Anything).Return(nil, errCheck)
			},
			wantErr: true,
			errMsg:  "health check failed: check error",
		},
		{
			name:       "unhealthy report",
			jsonOutput: false,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *agenttest.MockServiceSessionDependencies, bus *agenttest.MockServiceEventBus, hcm *mockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetHealthManager").Return(hcm)
				bus.On("Shutdown", mock.Anything).Return(nil)

				hcm.On("CheckAll", mock.Anything).Return(unhealthyReport, nil)

				uir.On("IsTerminalContext").Return(false)
				uir.On("SetUseColor", false).Return()
				uir.On("RenderHealthReport", mock.Anything, unhealthyReport).Return()
			},
			wantErr: true,
			errMsg:  "system health check failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sf := &agentinternal.MockSessionLifecycleManager{}
			deps := &agenttest.MockServiceSessionDependencies{}
			bus := &agenttest.MockServiceEventBus{}
			hcm := &mockHealthCheckManager{}
			uir := &mockUIRendererForDiag{}

			if tt.setupMock != nil {
				tt.setupMock(sf, deps, bus, hcm, uir)
			}

			var stdout bytes.Buffer
			service := agent.NewChatService(
				"home", "v1", &stdout, io.Discard, nil,
				sf, nil, uir, nil, nil, nil,
			)

			cfg := &config.Config{Mode: "assistant"}
			err := service.RunDiagnostics(ctx, cfg, "config.yaml", tt.jsonOutput)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}

			if tt.checkOut != nil {
				tt.checkOut(t, stdout.String())
			}

			sf.AssertExpectations(t)
			deps.AssertExpectations(t)
			hcm.AssertExpectations(t)
			uir.AssertExpectations(t)
		})
	}
}

// mockHistoryBrowser is a mock implementation of ports.HistoryBrowser for testing.
type mockHistoryBrowser struct {
	mock.Mock
}

func (m *mockHistoryBrowser) Browse(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	return m.Called(ctx, provider, hManager).Error(0)
}

func TestBrowseHistory(t *testing.T) {
	tests := []struct {
		name      string
		browseErr error
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "success",
			browseErr: nil,
			wantErr:   false,
		},
		{
			name:      "browser error",
			browseErr: errors.New("browser failed"),
			wantErr:   true,
			errMsg:    "browser failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			browser := &mockHistoryBrowser{}
			mockHM := &mockHistoryManagerForRetry{}

			browser.On("Browse", ctx, mock.Anything, mockHM).Return(tt.browseErr)

			service := agent.NewChatService(
				"home", "v1", io.Discard, io.Discard, nil,
				nil, nil, nil, nil, browser, nil,
			)

			err := service.BrowseHistory(ctx, nil, mockHM)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}

			browser.AssertExpectations(t)
		})
	}
}

// mockToolRegistry is a minimal mock of tools.Registry for testing GetToolNames.
type mockToolRegistry struct {
	declarations []*tools.ToolDeclaration
}

func (m *mockToolRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}
func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}
func (m *mockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}
func (m *mockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}
func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (m *mockToolRegistry) IsSerial(name string) bool                { return false }
func (m *mockToolRegistry) IsLongRunning(name string) bool           { return false }
func (m *mockToolRegistry) GetOptions(name string) tools.ToolOptions { return tools.ToolOptions{} }
func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.declarations
}
func (m *mockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration { return nil }
func (m *mockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}
func (m *mockToolRegistry) ListAvailableToolkits() []string { return nil }

func TestGetToolNames(t *testing.T) {
	tests := []struct {
		name         string
		declarations []*tools.ToolDeclaration
		wantNames    []string
	}{
		{
			name: "multiple tools",
			declarations: []*tools.ToolDeclaration{
				{Name: "read_files"},
				{Name: "write_file"},
				{Name: "execute_command"},
			},
			wantNames: []string{"read_files", "write_file", "execute_command"},
		},
		{
			name:         "empty registry",
			declarations: []*tools.ToolDeclaration{},
			wantNames:    []string{},
		},
		{
			name: "single tool",
			declarations: []*tools.ToolDeclaration{
				{Name: "search"},
			},
			wantNames: []string{"search"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := &mockToolRegistry{declarations: tt.declarations}

			service := agent.NewChatService(
				"home", "v1", io.Discard, io.Discard, nil,
				nil, nil, nil, nil, nil, nil,
			)

			names, err := service.GetToolNames(ctx, reg)

			assert.NoError(t, err)
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestChatService_StreamTurnsLog_EmptyMode(t *testing.T) {
	ctx := context.Background()

	// Verifies that an empty Config.Mode falls back to "default" mode in path
	// resolution.  The call proceeds to LogOpener.Open for the default-mode
	// path, which fails with os.ErrNotExist, yielding a graceful "No turns log
	// found" message.  (The empty-path guard — service.go:214 — is tested
	// separately in TestChatService_StreamTurnsLog_EmptyPath.)
	mFS := new(mockFileSystemStream)
	logPath := persistence.ResolvePaths("/nonexistent", "").TurnsLogPath
	mFS.On("Open", mock.Anything, logPath).Return(nil, os.ErrNotExist)

	service := agent.NewChatService(
		"/nonexistent", "v1", io.Discard, io.Discard, nil,
		nil, nil, nil, nil, nil, mFS,
	)

	var out bytes.Buffer
	cfg := &config.Config{Mode: ""}
	err := service.StreamTurnsLog(ctx, cfg, &out)

	// Empty mode → default mode; file doesn't exist → graceful message, no error.
	assert.NoError(t, err)
	assert.Equal(t, "No turns log found for this session yet.\n", out.String())
	mFS.AssertExpectations(t)
}

func TestChatService_StreamTurnsLog_EmptyPath(t *testing.T) {
	ctx := context.Background()

	// Inject a resolvePaths stub that returns a zero-value Paths struct
	// (all fields empty, including TurnsLogPath). This exercises the
	// defensive guard at service.go:214 which is otherwise unreachable
	// through the real persistence.ResolvePaths.
	origResolve := agent.PathResolveFunc
	agent.PathResolveFunc = func(homeDir, mode string) *persistence.Paths {
		return &persistence.Paths{}
	}
	t.Cleanup(func() {
		agent.PathResolveFunc = origResolve
	})

	// LogOpener must not be called — the guard should short-circuit before Open.
	service := agent.NewChatService(
		"/test", "v1", io.Discard, io.Discard, nil,
		nil, nil, nil, nil, nil, nil,
	)

	var out bytes.Buffer
	cfg := &config.Config{Mode: "assistant"}
	err := service.StreamTurnsLog(ctx, cfg, &out)

	assert.Error(t, err)
	assert.Equal(t, "turns log path not available", err.Error())
}
