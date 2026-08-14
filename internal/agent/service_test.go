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
)

// newTestChatService builds a ChatService with default test wiring; callers
// override individual config fields via the variadic functional overrides.
func newTestChatService(t *testing.T, overrides ...func(*ports.ChatServiceConfig)) ports.ChatService {
	t.Helper()
	cfg := ports.ChatServiceConfig{
		HomeDir: "home",
		Version: "v1",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}
	for _, o := range overrides {
		o(&cfg)
	}
	return agent.NewChatService(cfg)
}

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
	service ports.ChatService,
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

	service = newTestChatService(t, func(c *ports.ChatServiceConfig) {
		c.Stderr = stderr
		c.SM = sm
		c.LifecycleManager = sf
		c.ChatterFactory = chatterFactory
		c.UIRenderer = &agenttest.StubUIRenderer{}
		c.HistoryRenderer = &agenttest.StubHistoryRenderer{}
		c.HistoryBrowser = &agenttest.StubHistoryBrowser{}
	})

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
	sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return deps, hm, cleanup, nil
	}

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
	sf.FinalizeSessionFunc = func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, c *config.Config) error {
		return nil
	}

	deps.EventBus = bus
	deps.Paths = &persistence.Paths{TurnsLogPath: "turns.log"}
	deps.HistoryManager = mockHM
	deps.Logger = &ports.NoOpLogger{}
	deps.TurnsLogger = tl
	deps.SessionProvider = nil

	baseSetup(sf, agentMock, capturer, deps, mockHM, cleanup, cfg)

	tl.CloseFunc = func() error { return nil }

	cmd := ports.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.NoError(t, err)
	assert.True(t, cleanupCalled, "cleanup should have been called")

	snap := sf.Snapshot()
	assert.Equal(t, 1, snap["BuildSessionDependencies"])
	assert.Equal(t, 1, snap["FinalizeSession"])
}

// TestProcessMessage_BuildError verifies that a BuildSessionDependencies
// failure is returned directly without any further processing.
func TestProcessMessage_BuildError(t *testing.T) {
	errBuild := errors.New("build error")
	sf, _, capturer, _, _, _, _, service := newProcessMessageFixtures(t, io.Discard)

	cfg := &config.Config{Mode: "assistant"}
	sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return nil, nil, func(context.Context) error { return nil }, errBuild
	}

	cmd := ports.ChatCommand{ConfigPath: "config.yaml"}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "build error")
	assert.ErrorIs(t, err, errBuild)

	snap := sf.Snapshot()
	assert.Equal(t, 1, snap["BuildSessionDependencies"])
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
			sf.FinalizeSessionFunc = func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, c *config.Config) error {
				return nil
			}

			deps.EventBus = bus
			deps.Paths = &persistence.Paths{TurnsLogPath: "turns.log"}
			deps.HistoryManager = mockHM
			deps.Logger = &ports.NoOpLogger{}
			deps.TurnsLogger = tl
			deps.SessionProvider = nil

			baseSetup(sf, agentMock, capturer, deps, mockHM, cleanup, sharedCfg)

			bus.ShutdownErr = tt.busShutdownErr

			cmd := ports.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"}
			err := service.ProcessMessage(context.Background(), sharedCfg, cmd, capturer)

			assert.NoError(t, err)
			assert.Contains(t, stderr.String(), tt.stderrContains)

			snap := sf.Snapshot()
			assert.Equal(t, 1, snap["BuildSessionDependencies"])
			assert.Equal(t, 1, snap["FinalizeSession"])
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
	sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return deps, mockHM, cleanup, nil
	}
	sf.FinalizeSessionFunc = func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, c *config.Config) error {
		return nil
	}

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

	cmd := ports.ChatCommand{ConfigPath: "config.yaml", Retry: true}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.NoError(t, err)
	assert.True(t, cleanupCalled, "cleanup should have been called")

	snap := sf.Snapshot()
	assert.Equal(t, 1, snap["BuildSessionDependencies"])
	assert.Equal(t, 1, snap["FinalizeSession"])
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
	sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
		return deps, mockHM, cleanup, nil
	}

	deps.EventBus = bus

	capturer.ConfirmResult = false

	cmd := ports.ChatCommand{ConfigPath: "config.yaml", Retry: true}
	err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

	assert.NoError(t, err)

	snap := sf.Snapshot()
	assert.Equal(t, 1, snap["BuildSessionDependencies"])
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

			sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
				return deps, mockHM, cleanup, nil
			}

			deps.EventBus = bus

			capturer.ConfirmResult = false
			capturer.ConfirmErr = tt.confirmErr

			cmd := ports.ChatCommand{ConfigPath: "config.yaml", Retry: true}
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

			sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
				return deps, mockHM, cleanup, nil
			}
			sf.FinalizeSessionFunc = func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, c *config.Config) error {
				return errFinalize
			}

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

			cmd := ports.ChatCommand{ConfigPath: "config.yaml", Prompt: "hello"}
			err := service.ProcessMessage(context.Background(), cfg, cmd, capturer)

			assert.Error(t, err)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr)
			}
			if tt.extraExpectedErr != nil {
				assert.ErrorIs(t, err, tt.extraExpectedErr)
			}

			snap := sf.Snapshot()
			assert.Equal(t, 1, snap["BuildSessionDependencies"])
			assert.Equal(t, 1, snap["FinalizeSession"])
		})
	}
}

func TestGetLastUserMessage(t *testing.T) {
	ctx := context.Background()
	sm := &agenttest.MockServiceSecurityManager{}

	mockHM := &mockHistoryManagerForRetry{msg: "last message", turns: 1}

	service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
		c.SM = sm
	})

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
func (m *mockHistoryManagerForRetry) SetPinned(ctx context.Context, turnID string, pinned bool) error {
	return nil
}
func (m *mockHistoryManagerForRetry) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

func (m *mockHistoryManagerForRetry) GetFilePath() string { return "" }

func (m *mockHistoryManagerForRetry) GetLastModelTurn(ctx context.Context) (int, *llm.Content, error) {
	return 0, nil, ports.ErrHistoryNotFound
}

func (m *mockHistoryManagerForRetry) GetModelTurn(ctx context.Context, index int) (*llm.Content, error) {
	return nil, ports.ErrHistoryNotFound
}

func (m *mockHistoryManagerForRetry) UpdateTurnContent(ctx context.Context, index int, newText string, newThought string) error {
	return nil
}

type mockFileSystemStream struct {
	persistence.FileSystem
	OpenFunc func(ctx context.Context, name string) (persistence.File, error)
}

func (m *mockFileSystemStream) Open(ctx context.Context, name string) (persistence.File, error) {
	if m.OpenFunc != nil {
		return m.OpenFunc(ctx, name)
	}
	return nil, nil
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
				mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
					return &minimalFile{Reader: strings.NewReader("turn 1: hello")}, nil
				}
			},
			expectedOut: "turn 1: hello",
			wantErr:     false,
		},
		{
			name: "LogFileMissing",
			mode: "developer",
			setupMock: func(mFS *mockFileSystemStream) {
				mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
					return nil, os.ErrNotExist
				}
			},
			expectedOut: "No turns log found for this session yet.\n",
			wantErr:     false,
		},
		{
			name: "PermissionDenied",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
					return nil, os.ErrPermission
				}
			},
			wantErr: true,
			errMsg:  "failed to open turns log",
		},
		{
			name: "ReadError",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
					return &minimalFile{Reader: &errorReader{}}, nil
				}
			},
			wantErr: true,
			errMsg:  "failed to stream log",
		},
		{
			name: "CloseError",
			mode: "assistant",
			setupMock: func(mFS *mockFileSystemStream) {
				mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
					return &minimalFile{
						Reader:   strings.NewReader("log content"),
						closeErr: errors.New("close failure"),
					}, nil
				}
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

			service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
				c.HomeDir = homeDir
				c.LogOpener = mFS
			})

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
	SetUseColorFunc        func(use bool)
	IsTerminalContextFunc  func() bool
	RenderHealthReportFunc func(ctx context.Context, report *ports.HealthReport)
}

func (m *mockUIRendererForDiag) SetUseColor(use bool) {
	if m.SetUseColorFunc != nil {
		m.SetUseColorFunc(use)
	}
}
func (m *mockUIRendererForDiag) IsTerminalContext() bool {
	if m.IsTerminalContextFunc != nil {
		return m.IsTerminalContextFunc()
	}
	return false
}
func (m *mockUIRendererForDiag) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {
	if m.RenderHealthReportFunc != nil {
		m.RenderHealthReportFunc(ctx, report)
	}
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
		name           string
		jsonOutput     bool
		expectCheckAll bool
		setupMock      func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag)
		wantErr        bool
		errMsg         string
		checkOut       func(t *testing.T, stdout string)
	}{
		{
			name:           "success UI output",
			jsonOutput:     false,
			expectCheckAll: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cleanup := func(context.Context) error { return nil }
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return deps, nil, cleanup, nil
				}

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) {
					return healthyReport, nil
				}

				uir.IsTerminalContextFunc = func() bool { return false }
				uir.SetUseColorFunc = func(use bool) {}
				uir.RenderHealthReportFunc = func(ctx context.Context, report *ports.HealthReport) {}
			},
		},
		{
			name:           "success JSON output",
			jsonOutput:     true,
			expectCheckAll: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cleanup := func(context.Context) error { return nil }
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return deps, nil, cleanup, nil
				}

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) {
					return healthyReport, nil
				}
			},
			checkOut: func(t *testing.T, stdout string) {
				t.Helper()
				assert.Contains(t, stdout, `"overall_status": "healthy"`)
				assert.Contains(t, stdout, `"persistence"`)
			},
		},
		{
			name:           "JSON marshal error",
			jsonOutput:     true,
			expectCheckAll: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cleanup := func(context.Context) error { return nil }
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return deps, nil, cleanup, nil
				}

				deps.EventBus = bus
				deps.HealthManager = hcm

				// Construct a report with an un-marshalable Details field.
				// json.MarshalIndent cannot serialize a channel, so the defensive
				// guard is exercised.
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
				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) {
					return unmarshalableReport, nil
				}
			},
			wantErr: true,
			errMsg:  "failed to serialize health report",
		},
		{
			name: "build deps error",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return nil, nil, nil, errBuild
				}
			},
			wantErr: true,
			errMsg:  "build error",
		},
		{
			name: "nil health manager",
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cleanup := func(context.Context) error { return nil }
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return deps, nil, cleanup, nil
				}

				deps.EventBus = bus
			},
			wantErr: true,
			errMsg:  "health check manager not available",
		},
		{
			name:           "CheckAll error",
			expectCheckAll: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cleanup := func(context.Context) error { return nil }
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return deps, nil, cleanup, nil
				}

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) {
					return nil, errCheck
				}
			},
			wantErr: true,
			errMsg:  "health check failed: check error",
		},
		{
			name:           "unhealthy report",
			jsonOutput:     false,
			expectCheckAll: true,
			setupMock: func(sf *agentinternal.MockSessionLifecycleManager, deps *stubDiagComposer, bus *agenttest.StubEventBus, hcm *agenttest.MockHealthCheckManager, uir *mockUIRendererForDiag) {
				cleanup := func(context.Context) error { return nil }
				sf.BuildSessionDepsFunc = func(ctx context.Context, c *config.Config, configPath string, newSession bool, capturer ports.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
					return deps, nil, cleanup, nil
				}

				deps.EventBus = bus
				deps.HealthManager = hcm

				hcm.CheckAllFunc = func(ctx context.Context) (*ports.HealthReport, error) {
					return unhealthyReport, nil
				}

				uir.IsTerminalContextFunc = func() bool { return false }
				uir.SetUseColorFunc = func(use bool) {}
				uir.RenderHealthReportFunc = func(ctx context.Context, report *ports.HealthReport) {}
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
			service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
				c.Stdout = &stdout
				c.LifecycleManager = sf
				c.UIRenderer = uir
			})

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

			snap := sf.Snapshot()
			assert.Equal(t, 1, snap["BuildSessionDependencies"])
			checkAllCalls, _, _ := hcm.Snapshot()
			if tt.expectCheckAll {
				assert.Equal(t, 1, checkAllCalls)
			} else {
				assert.Equal(t, 0, checkAllCalls)
			}
		})
	}
}

// mockHistoryBrowser is a mock implementation of ports.HistoryBrowser for testing.
type mockHistoryBrowser struct {
	BrowseFunc func(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error
}

func (m *mockHistoryBrowser) Browse(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	if m.BrowseFunc != nil {
		return m.BrowseFunc(ctx, provider, hManager)
	}
	return nil
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

			browser.BrowseFunc = func(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
				return tt.browseErr
			}

			service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
				c.HistoryBrowser = browser
			})

			err := service.BrowseHistory(ctx, nil, mockHM)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
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

			service := newTestChatService(t)

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
	// found" message.  (The empty-path guard is tested separately in
	// TestChatService_StreamTurnsLog_EmptyPath.)
	mFS := new(mockFileSystemStream)
	mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
		return nil, os.ErrNotExist
	}

	service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
		c.HomeDir = "/nonexistent"
		c.LogOpener = mFS
	})

	var out bytes.Buffer
	cfg := &config.Config{Mode: ""}
	err := service.StreamTurnsLog(ctx, cfg, &out)

	// Empty mode → default mode; file doesn't exist → graceful message, no error.
	assert.NoError(t, err)
	assert.Equal(t, "No turns log found for this session yet.\n", out.String())
}

func TestChatService_StreamTurnsLog_EmptyPath(t *testing.T) {
	ctx := context.Background()

	// Inject a resolvePaths stub via the ResolvePaths config field that returns
	// a zero-value Paths struct (all fields empty, including TurnsLogPath).
	// This exercises the defensive guard which is otherwise unreachable through
	// the real persistence.ResolvePaths.
	emptyPathResolver := func(homeDir, mode string) *persistence.Paths {
		return &persistence.Paths{}
	}

	// LogOpener must not be called — the guard should short-circuit before Open.
	// Passing nil for LogOpener proves this: if Open is reached, the test panics.
	service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
		c.HomeDir = "/test"
		c.ResolvePaths = emptyPathResolver
	})

	var out bytes.Buffer
	cfg := &config.Config{Mode: "assistant"}
	err := service.StreamTurnsLog(ctx, cfg, &out)

	assert.Error(t, err)
	assert.Equal(t, "turns log path not available", err.Error())
}

func TestChatService_ResolvePathsDefault(t *testing.T) {
	ctx := context.Background()
	mFS := new(mockFileSystemStream)
	var gotName string
	mFS.OpenFunc = func(ctx context.Context, name string) (persistence.File, error) {
		gotName = name
		return nil, os.ErrNotExist // graceful: proves Open was reached with the resolved path
	}
	service := newTestChatService(t, func(c *ports.ChatServiceConfig) { c.LogOpener = mFS })
	var out bytes.Buffer
	err := service.StreamTurnsLog(ctx, &config.Config{Mode: "assistant"}, &out)
	assert.NoError(t, err)
	assert.Equal(t, "No turns log found for this session yet.\n", out.String())
	want := persistence.ResolvePaths("home", "assistant").TurnsLogPath
	assert.Equal(t, want, gotName)
}

func TestUpdateLastTurn(t *testing.T) {
	ctx := context.Background()

	service := newTestChatService(t, func(c *ports.ChatServiceConfig) {
		c.SM = &agenttest.MockServiceSecurityManager{}
	})

	t.Run("delete when text is empty", func(t *testing.T) {
		hm := &agenttest.MockHistoryManager{}
		hm.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "q1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "a1"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "q2"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "a2"}}},
		})

		err := service.UpdateLastTurn(ctx, hm, "")

		assert.NoError(t, err)
		assert.Equal(t, 2, hm.GetTotalEntries())
	})

	t.Run("replace when text is non-empty", func(t *testing.T) {
		var gotIdx int
		var gotText string
		hm := &agenttest.MockHistoryManager{
			GetLastModelTurnFunc: func(ctx context.Context) (int, *llm.Content, error) {
				return 3, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "old"}}}, nil
			},
			UpdateTurnContentFunc: func(ctx context.Context, index int, newText, newThought string) error {
				gotIdx = index
				gotText = newText
				return nil
			},
		}

		err := service.UpdateLastTurn(ctx, hm, "new response")

		assert.NoError(t, err)
		assert.Equal(t, 3, gotIdx)
		assert.Equal(t, "new response", gotText)
	})

	t.Run("error when GetLastModelTurn fails", func(t *testing.T) {
		hm := &agenttest.MockHistoryManager{
			GetLastModelTurnFunc: func(ctx context.Context) (int, *llm.Content, error) {
				return 0, nil, errors.New("no model turns")
			},
		}

		err := service.UpdateLastTurn(ctx, hm, "some text")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "update last turn")
	})

	t.Run("error when RollbackTurns fails on delete", func(t *testing.T) {
		hm := &agenttest.MockHistoryManager{}
		hm.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "q1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "a1"}}},
		})
		hm.SetRollbackErr(errors.New("rollback failed"))

		err := service.UpdateLastTurn(ctx, hm, "")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "update last turn (delete)")
		assert.Contains(t, err.Error(), "rollback failed")
	})

	t.Run("error when UpdateTurnContent fails", func(t *testing.T) {
		hm := &agenttest.MockHistoryManager{
			GetLastModelTurnFunc: func(ctx context.Context) (int, *llm.Content, error) {
				return 1, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "old"}}}, nil
			},
			UpdateTurnContentFunc: func(ctx context.Context, index int, newText, newThought string) error {
				return errors.New("update failed")
			},
		}

		err := service.UpdateLastTurn(ctx, hm, "new text")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "update last turn")
		assert.Contains(t, err.Error(), "update failed")
	})
}
