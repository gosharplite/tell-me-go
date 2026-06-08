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
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// newProcessMessageFixtures creates the 7 mocks, chatterFactory, and ChatService
// instance used by all TestProcessMessage_* sub-functions. Callers set per-case
// expectations on the returned mocks and then invoke service.ProcessMessage.
func newProcessMessageFixtures(
	t *testing.T,
	stderr io.Writer,
) (
	sf *agentinternal.MockSessionLifecycleManager,
	sm *agenttest.MockServiceSecurityManager,
	capturer *agenttest.StubCapturer,
	deps *agenttest.StubChatterComposer,
	bus *agenttest.StubEventBus,
	agentMock *agenttest.MockServiceAgent,
	tl *agenttest.MockTurnsLogger,
	service agent.ChatService,
) {
	t.Helper()

	sf = &agentinternal.MockSessionLifecycleManager{}
	sm = &agenttest.MockServiceSecurityManager{}
	capturer = &agenttest.StubCapturer{}
	deps = &agenttest.StubChatterComposer{}
	bus = &agenttest.StubEventBus{}
	agentMock = &agenttest.MockServiceAgent{}
	tl = &agenttest.MockTurnsLogger{}

	chatterFactory := ports.ChatterFactory(func(ctx context.Context, sd ports.ChatterComposer, cCfg ports.ChatterConfig) (ports.Chatter, error) {
		return agentMock, nil
	})

	service = agent.NewChatService(
		"home", "v1", io.Discard, stderr, sm,
		sf, chatterFactory, &agenttest.StubUIRenderer{}, &agenttest.StubHistoryRenderer{}, &agenttest.StubHistoryBrowser{}, nil,
	)

	return
}

// baseSetup configures the common "happy path" mock expectations shared
// across most TestProcessMessage table rows. Individual rows only declare
// their deltas (e.g., overriding bus.Shutdown or cleanup behavior).
func baseSetup(
	sf *agentinternal.MockSessionLifecycleManager,
	agentMock *agenttest.MockServiceAgent,
	cap *agenttest.StubCapturer,
	deps ports.ChatterComposer,
	hm ports.HistoryManager,
	cleanup func(context.Context) error,
	cfg *config.Config,
) {
	sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).
		Return(deps, hm, cleanup, nil)

	agentMock.SetLimitsFunc = func(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
		return nil
	}
	agentMock.SubscribeFunc = func(handler func(context.Context, events.Event)) {}
	agentMock.ChatFunc = func(ctx context.Context, sess *ports.Session, prompt string) error {
		return nil
	}
	agentMock.ShutdownFunc = func(ctx context.Context) error {
		return nil
	}

	cap.IsTTYVal = true
}

// TestProcessMessage_Success verifies the happy path: a single prompt
// completes without errors, cleanup is called, and all mocks are satisfied.
func TestProcessMessage_Success(t *testing.T) {
	sf, _, capturer, deps, bus, agentMock, tl, service := newProcessMessageFixtures(t, io.Discard)

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
	sf.On("FinalizeSession", mock.Anything, mock.Anything, mock.Anything, cfg).Return(nil)

	deps.EventBus = bus
	deps.Paths = &persistence.Paths{TurnsLogPath: "turns.log"}
	deps.HistoryManager = mockHM
	deps.Logger = &ports.NoOpLogger{}
	deps.TurnsLogger = tl
	deps.SessionProvider = nil

	baseSetup(sf, agentMock, capturer, deps, mockHM, cleanup, cfg)

	tl.CloseFunc = func() error { return nil }

	cmd := agent.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.NoError(t, err)
	assert.True(t, cleanupCalled, "cleanup should have been called")

	sf.AssertExpectations(t)
}

// TestProcessMessage_BuildError verifies that a BuildSessionDependencies
// failure is returned directly without any further processing.
func TestProcessMessage_BuildError(t *testing.T) {
	errBuild := errors.New("build error")
	sf, _, capturer, _, _, _, _, service := newProcessMessageFixtures(t, io.Discard)

	cfg := &config.Config{Mode: "assistant"}
	sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, capturer).
		Return(nil, nil, func(context.Context) error { return nil }, errBuild)

	cmd := agent.ChatCommand{ConfigPath: "config.yaml"}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "build error")
	assert.ErrorIs(t, err, errBuild)

	sf.AssertExpectations(t)
}

// TestProcessMessage_CleanupErrors verifies that cleanup and event-bus
// shutdown failures are logged to stderr without failing the operation.
func TestProcessMessage_CleanupErrors(t *testing.T) {
	sharedCfg := &config.Config{
		Mode: "assistant",
		Providers: map[string]config.LLMProvider{
			"test": {Model: "test-model"},
		},
		SelectedProvider: "test",
	}

	tests := []struct {
		name           string
		cleanupErr     error
		busShutdownErr error
		stderrContains string
	}{
		{
			name:           "CleanupError",
			cleanupErr:     errors.New("cleanup failure"),
			busShutdownErr: nil,
			stderrContains: "Warning: Session cleanup failed: cleanup failure",
		},
		{
			name:           "EventBusShutdownError",
			cleanupErr:     nil,
			busShutdownErr: errors.New("bus shutdown failure"),
			stderrContains: "Warning: Event bus shutdown failed: bus shutdown failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := &bytes.Buffer{}
			sf, _, capturer, deps, bus, agentMock, tl, service := newProcessMessageFixtures(t, stderr)

			cleanup := func(context.Context) error {
				return tt.cleanupErr
			}

			mockHM := &mockHistoryManagerForRetry{}
			sf.On("FinalizeSession", mock.Anything, mock.Anything, mock.Anything, sharedCfg).Return(nil)

			deps.EventBus = bus
			deps.Paths = &persistence.Paths{TurnsLogPath: "turns.log"}
			deps.HistoryManager = mockHM
			deps.Logger = &ports.NoOpLogger{}
			deps.TurnsLogger = tl
			deps.SessionProvider = nil

			baseSetup(sf, agentMock, capturer, deps, mockHM, cleanup, sharedCfg)

			bus.ShutdownErr = tt.busShutdownErr

			cmd := agent.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"}
			err := service.ProcessMessage(context.Background(), sharedCfg, cmd, capturer)

			assert.NoError(t, err)
			assert.Contains(t, stderr.String(), tt.stderrContains)

			sf.AssertExpectations(t)
		})
	}
}

// TestProcessMessage_RetrySuccess verifies the full retry happy path:
// history lookup → user confirmation → re-issue prompt → cleanup.
func TestProcessMessage_RetrySuccess(t *testing.T) {
	sf, _, capturer, deps, bus, agentMock, tl, service := newProcessMessageFixtures(t, io.Discard)

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
	sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, capturer).
		Return(deps, mockHM, cleanup, nil)
	sf.On("FinalizeSession", mock.Anything, mock.Anything, mock.Anything, cfg).Return(nil)

	deps.EventBus = bus
	deps.Paths = &persistence.Paths{TurnsLogPath: "turns.log"}
	deps.HistoryManager = mockHM
	deps.Logger = &ports.NoOpLogger{}
	deps.TurnsLogger = tl
	deps.SessionProvider = nil

	tl.CloseFunc = func() error { return nil }

	agentMock.SetLimitsFunc = func(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
		return nil
	}
	agentMock.SubscribeFunc = func(handler func(context.Context, events.Event)) {}
	agentMock.ChatFunc = func(ctx context.Context, sess *ports.Session, prompt string) error {
		return nil
	}
	agentMock.ShutdownFunc = func(ctx context.Context) error {
		return nil
	}

	capturer.ConfirmResult = true
	capturer.IsTTYVal = true

	cmd := agent.ChatCommand{ConfigPath: "config.yaml", Retry: true}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.NoError(t, err)
	assert.True(t, cleanupCalled, "cleanup should have been called")

	sf.AssertExpectations(t)
}

// TestProcessMessage_RetryAborted verifies that when the user declines
// the retry confirmation, the operation exits cleanly without invoking
// the agent.
func TestProcessMessage_RetryAborted(t *testing.T) {
	sf, _, capturer, deps, bus, _, _, service := newProcessMessageFixtures(t, io.Discard)

	cfg := &config.Config{
		Mode: "assistant",
		Providers: map[string]config.LLMProvider{
			"test": {Model: "test-model"},
		},
		SelectedProvider: "test",
	}

	cleanup := func(context.Context) error { return nil }
	mockHM := &mockHistoryManagerForRetry{msg: "retry this", turns: 2}
	sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, capturer).
		Return(deps, mockHM, cleanup, nil)

	deps.EventBus = bus

	capturer.ConfirmResult = false

	cmd := agent.ChatCommand{ConfigPath: "config.yaml", Retry: true}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.NoError(t, err)

	sf.AssertExpectations(t)
}

// TestProcessMessage_RetryErrors verifies error paths during retry
// orchestration: no history, history lookup failure, and confirmation
// UI error.
func TestProcessMessage_RetryErrors(t *testing.T) {
	cfg := &config.Config{Mode: "assistant"}

	tests := []struct {
		name       string
		hmMsg      string
		hmTurns    int
		hmErr      error
		confirmErr error
		errMsg     string
	}{
		{
			name:    "RetryNoHistory",
			hmMsg:   "",
			hmTurns: 0,
			errMsg:  "no previous user message found to retry",
		},
		{
			name:   "RetryHistoryError",
			hmErr:  errors.New("db error"),
			errMsg: "failed to get last user message for retry",
		},
		{
			name:       "RetryConfirmError",
			hmMsg:      "retry me",
			hmTurns:    1,
			confirmErr: errors.New("UI error"),
			errMsg:     "UI error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sf, _, capturer, deps, bus, _, _, service := newProcessMessageFixtures(t, io.Discard)

			cleanup := func(context.Context) error { return nil }
			mockHM := &mockHistoryManagerForRetry{
				msg:   tt.hmMsg,
				turns: tt.hmTurns,
				err:   tt.hmErr,
			}

			sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, capturer).
				Return(deps, mockHM, cleanup, nil)

			deps.EventBus = bus

			capturer.ConfirmResult = false
			capturer.ConfirmErr = tt.confirmErr

			cmd := agent.ChatCommand{ConfigPath: "config.yaml", Retry: true}
			err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestProcessMessage_FinalizeErrors verifies that FinalizeSession failures
// are propagated correctly, including the joined-error path when both
// the chat run and finalization fail.
func TestProcessMessage_FinalizeErrors(t *testing.T) {
	errFinalize := errors.New("finalize error")
	errChat := errors.New("chat error")

	cfg := &config.Config{
		Mode: "assistant",
		Providers: map[string]config.LLMProvider{
			"test": {Model: "test-model"},
		},
		SelectedProvider: "test",
	}

	tests := []struct {
		name             string
		chatErr          error
		expectedErr      error
		extraExpectedErr error
	}{
		{
			name:        "FinalizeErrorOnly",
			chatErr:     nil,
			expectedErr: errFinalize,
		},
		{
			name:             "DoubleError",
			chatErr:          errChat,
			expectedErr:      errChat,
			extraExpectedErr: errFinalize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sf, _, capturer, deps, bus, agentMock, tl, service := newProcessMessageFixtures(t, io.Discard)

			cleanup := func(context.Context) error { return nil }
			mockHM := &mockHistoryManagerForRetry{}

			sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, capturer).
				Return(deps, mockHM, cleanup, nil)
			sf.On("FinalizeSession", mock.Anything, mock.Anything, mock.Anything, cfg).Return(errFinalize)

			deps.EventBus = bus
			deps.Paths = &persistence.Paths{TurnsLogPath: "turns.log"}
			deps.HistoryManager = mockHM
			deps.Logger = &ports.NoOpLogger{}
			deps.TurnsLogger = tl
			deps.SessionProvider = nil

			agentMock.SetLimitsFunc = func(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
				return nil
			}
			agentMock.SubscribeFunc = func(handler func(context.Context, events.Event)) {}
			agentMock.ChatFunc = func(ctx context.Context, sess *ports.Session, prompt string) error {
				return tt.chatErr
			}
			agentMock.ShutdownFunc = func(ctx context.Context) error {
				return nil
			}

			capturer.IsTTYVal = true

			cmd := agent.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"}
			err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

			assert.Error(t, err)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr)
			}
			if tt.extraExpectedErr != nil {
				assert.ErrorIs(t, err, tt.extraExpectedErr)
			}

			sf.AssertExpectations(t)
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

// stubDiagComposer wraps a StubChatterComposer and adds GetHealthManager
// for RunDiagnostics tests that need to provide a HealthCheckManager.
type stubDiagComposer struct {
	*agenttest.StubChatterComposer
	HealthManager ports.HealthCheckManager
}

func (s *stubDiagComposer) GetHealthManager() ports.HealthCheckManager { return s.HealthManager }

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
		setupMock  func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag)
		wantErr    bool
		errMsg     string
		checkOut   func(t *testing.T, stdout string)
	}{
		{
			name:       "success UI output",
			jsonOutput: false,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) { return healthyReport, nil }

				uir.On("IsTerminalContext").Return(false)
				uir.On("SetUseColor", false).Return()
				uir.On("RenderHealthReport", mock.Anything, healthyReport).Return()
			},
		},
		{
			name:       "success JSON output",
			jsonOutput: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) { return healthyReport, nil }
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
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.EventBus = bus
				deps.HealthManager = hcm

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
				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) { return unmarshalableReport, nil }
			},
			wantErr: true,
			errMsg:  "failed to serialize health report",
		},
		{
			name: "build deps error",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(nil, nil, (func(context.Context) error)(nil), errBuild)
			},
			wantErr: true,
			errMsg:  "build error",
		},
		{
			name: "nil health manager",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.EventBus = bus
			},
			wantErr: true,
			errMsg:  "health check manager not available",
		},
		{
			name: "CheckAll error",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) { return nil, errCheck }
			},
			wantErr: true,
			errMsg:  "health check failed: check error",
		},
		{
			name:       "unhealthy report",
			jsonOutput: false,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cfg := &config.Config{Mode: "assistant"}
				cleanup := func(context.Context) error { return nil }
				sf.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, nil).Return(deps, nil, cleanup, nil)

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) { return unhealthyReport, nil }

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
			deps := &stubDiagComposer{StubChatterComposer: &agenttest.StubChatterComposer{}}
			bus := &agenttest.StubEventBus{}
			hcm := &agenttest.MockHealthCheckManager{}
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

	// Inject a resolvePaths stub via WithPathResolver that returns a
	// zero-value Paths struct (all fields empty, including TurnsLogPath).
	// This exercises the defensive guard at service.go:214 which is
	// otherwise unreachable through the real persistence.ResolvePaths.
	emptyPathResolver := func(homeDir, mode string) *persistence.Paths {
		return &persistence.Paths{}
	}

	// LogOpener must not be called — the guard should short-circuit before Open.
	// Passing nil for LogOpener proves this: if Open is reached, the test panics.
	service := agent.NewChatService(
		"/test", "v1", io.Discard, io.Discard, nil,
		nil, nil, nil, nil, nil, nil,
		agent.WithPathResolver(emptyPathResolver),
	)

	var out bytes.Buffer
	cfg := &config.Config{Mode: "assistant"}
	err := service.StreamTurnsLog(ctx, cfg, &out)

	assert.Error(t, err)
	assert.Equal(t, "turns log path not available", err.Error())
}
