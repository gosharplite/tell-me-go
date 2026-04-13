// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestProcessMessage(t *testing.T) {
	errBuild := errors.New("build error")
	errChat := errors.New("chat error")
	errFinalize := errors.New("finalize error")

	tests := []struct {
		name             string
		setupMock        func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error
		cmd              ChatCommand
		cfg              *config.Config
		wantErr          bool
		errMsg           string
		expectedErr      error
		extraExpectedErr error
	}{
		{
			name: "Success",
			cmd:  ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
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
				deps.On("GetLogger").Return(&ports.NoOpLogger{})
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
			cmd:  ChatCommand{ConfigPath: "config.yaml"},
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
			cmd:  ChatCommand{ConfigPath: "config.yaml", Retry: true},
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
				deps.On("GetLogger").Return(&ports.NoOpLogger{})
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
			cmd:  ChatCommand{ConfigPath: "config.yaml", Retry: true},
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
		{
			name: "RetryNoHistory",
			cmd:  ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
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
			cmd:  ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
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
			cmd:  ChatCommand{ConfigPath: "config.yaml", Retry: true},
			cfg:  &config.Config{Mode: "assistant"},
			setupMock: func(sf *mockSessionLifecycleManager, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent, tl *mockTurnsLogger) func(context.Context) error {
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
			cmd:  ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
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

				agent.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agent.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
				agent.On("Subscribe", mock.Anything).Return()
				agent.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
				agent.On("Shutdown", mock.Anything).Return(nil)

				cap.On("IsTTY", mock.Anything).Return(true)

				return nil
			},
			wantErr:     true,
			errMsg:      "",
			expectedErr: errFinalize,
		},
		{
			name: "DoubleError",
			cmd:  ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"},
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

				agent.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agent.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
				agent.On("Subscribe", mock.Anything).Return()
				agent.On("Chat", mock.Anything, mock.Anything, "hello").Return(errChat)
				agent.On("Shutdown", mock.Anything).Return(nil)

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

func (m *mockHistoryManagerForRetry) Sync(ctx context.Context) error {
	return nil
}
