// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockChatService struct {
	mock.Mock
	chatCalled bool
	lastParams agent.ChatOptions
}

func (m *mockChatService) ProcessMessage(ctx stdctx.Context, cfg *config.Config, opts agent.ChatOptions, capturer agent.CapturerInteractor) error {
	m.chatCalled = true
	m.lastParams = opts
	args := m.Called(ctx, cfg, opts, capturer)
	return args.Error(0)
}

func (m *mockChatService) GetLastUserMessage(ctx stdctx.Context, hManager ports.HistoryManager) (string, int, error) {
	return "retry test", 1, nil
}

func (m *mockChatService) BrowseHistory(ctx stdctx.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	return nil
}

func (m *mockChatService) GetToolNames(ctx stdctx.Context, reg tools.Registry) ([]string, error) {
	return []string{"test_tool"}, nil
}

func (m *mockChatService) StreamTurnsLog(ctx stdctx.Context, cfg *config.Config, out io.Writer) error {
	args := m.Called(ctx, cfg, out)
	return args.Error(0)
}

type mockBootstrapper struct {
	mock.Mock
}

func (m *mockBootstrapper) BuildSessionDependencies(ctx stdctx.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(stdctx.Context) error, error) {
	args := m.Called(ctx, cfg, configPath, newSession, capturer)
	var deps ports.SessionDependencies
	if args.Get(0) != nil {
		deps = args.Get(0).(ports.SessionDependencies)
	}
	var hManager ports.HistoryManager
	if args.Get(1) != nil {
		hManager = args.Get(1).(ports.HistoryManager)
	}
	return deps, hManager, args.Get(2).(func(stdctx.Context) error), args.Error(3)
}

func (m *mockBootstrapper) FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
	return m.Called(ctx, hManager, deps, cfg).Error(0)
}

func (m *mockBootstrapper) GetHistoryManager(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
	args := m.Called(ctx, cfg)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ports.HistoryManager), args.Error(1)
}

func (m *mockBootstrapper) GetUnifiedHistoryProvider(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	args := m.Called(ctx, cfg, hManager)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ports.UnifiedHistoryProvider), args.Error(1)
}

func (m *mockBootstrapper) GetSuggestionService(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
	args := m.Called(ctx, recentHistory)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(ports.SuggestionService), args.Error(1)
}

func (m *mockBootstrapper) GetAgentFactory() ports.ChatterFactory {
	return m.Called().Get(0).(ports.ChatterFactory)
}

func (m *mockBootstrapper) GetUIRenderer() ports.UIRenderer {
	return m.Called().Get(0).(ports.UIRenderer)
}

func (m *mockBootstrapper) GetHistoryRenderer() ports.HistoryRenderer {
	return m.Called().Get(0).(ports.HistoryRenderer)
}

func (m *mockBootstrapper) GetHistoryBrowser() ports.HistoryBrowser {
	return m.Called().Get(0).(ports.HistoryBrowser)
}

func (m *mockBootstrapper) GetChatService() agent.ChatService {
	return m.Called().Get(0).(agent.ChatService)
}

type mockLoader struct {
	mock.Mock
}

func (m *mockLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *mockLoader) Watch(ctx stdctx.Context, path string) (<-chan *config.Config, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(<-chan *config.Config), args.Error(1)
}

type mockSuggestionService struct{}

func (m *mockSuggestionService) GetSuggestions(ctx stdctx.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (m *mockSuggestionService) RecordPrompt(ctx stdctx.Context, prompt string) error { return nil }
func (m *mockSuggestionService) Close(ctx stdctx.Context) error                       { return nil }

type mockSM struct {
	domain_security.Manager
}

func (m *mockSM) SetInteractor(interactor domain_security.UserInteractor) {}
func (m *mockSM) TerminalLock()                                           {}
func (m *mockSM) TerminalUnlock()                                         {}
func (m *mockSM) Close() error                                            { return nil }

func setupMocks() (*mockBootstrapper, *mockLoader, *mockChatService) {
	mb := &mockBootstrapper{}
	ml := &mockLoader{}
	ms := &mockChatService{}
	ml.On("Load", mock.Anything).Return(&config.Config{}, nil).Maybe()
	mb.On("GetHistoryManager", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mb.On("GetSuggestionService", mock.Anything, mock.Anything).Return(&mockSuggestionService{}, nil).Maybe()
	ms.On("ProcessMessage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	return mb, ml, ms
}

func TestChatCommand_Execute(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
		MockPrompt:   "hello",
	}

	err := executeChatCommand(cmdCtx, []string{"hello"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.Prompt != "hello" {
		t.Errorf("expected prompt 'hello', got %q", mService.lastParams.Prompt)
	}
}

func TestChatCommand_Execute_LastN(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
	}

	err := executeChatCommand(cmdCtx, []string{"-l=5", "hello"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.LastN != 5 {
		t.Errorf("expected LastN 5, got %d", mService.lastParams.LastN)
	}
}

func TestChatCommand_Execute_BackN(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
	}

	err := executeChatCommand(cmdCtx, []string{"-b=2", "hello"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.BackN != 2 {
		t.Errorf("expected BackN 2, got %d", mService.lastParams.BackN)
	}
}

func TestChatCommand_Execute_Retry(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader("y\n"),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
	}

	err := executeChatCommand(cmdCtx, []string{"--retry"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.Prompt != "retry test" {
		t.Errorf("expected prompt 'retry test', got %q", mService.lastParams.Prompt)
	}

	if mService.lastParams.BackN != 1 {
		t.Errorf("expected BackN 1, got %d", mService.lastParams.BackN)
	}

	if !strings.Contains(stdout.String(), "Are you sure you want to retry") {
		t.Errorf("expected stdout to contain retry message, got %q", stdout.String())
	}
}

func TestChatCommand_Execute_Retry_Aborted(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader("n\n"),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
	}

	err := executeChatCommand(cmdCtx, []string{"--retry"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if mService.chatCalled {
		t.Error("expected chat service NOT to be called")
	}

	if !strings.Contains(stdout.String(), "Are you sure you want to retry") {
		t.Errorf("expected stdout to contain retry message, got %q", stdout.String())
	}
}

func TestChatCommand_Execute_TUIPrompt_SetsInteractor(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	var setInteractorCalled bool

	sm := &mockSM{}
	// Override SetInteractor locally using a mock struct that tracks calls
	trackingSM := &trackingMockSM{
		mockSM:          sm,
		setInteractorCb: func() { setInteractorCalled = true },
	}

	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader("hello\n"),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           trackingSM,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
		HomeDir:      t.TempDir(),
	}

	err := executeChatCommand(cmdCtx, []string{"--tui"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !setInteractorCalled {
		t.Error("expected SetInteractor to be called when TUI prompt is enabled")
	}
}

type trackingMockSM struct {
	*mockSM
	setInteractorCb func()
}

func (m *trackingMockSM) SetInteractor(interactor domain_security.UserInteractor) {
	if m.setInteractorCb != nil {
		m.setInteractorCb()
	}
}

func TestChatCommand_Execute_SuggestionServiceError_Fallback(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()
	mb.ExpectedCalls = nil
	mb.On("GetHistoryManager", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mb.On("GetSuggestionService", mock.Anything, mock.Anything).Return(nil, errors.New("initialization failed")).Maybe()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader("fallback test\n"),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
		HomeDir:      t.TempDir(),
	}

	err := executeChatCommand(cmdCtx, []string{"--tui"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stderr.String(), "Warning: failed to initialize suggestions: initialization failed") {
		t.Errorf("expected stderr to contain warning, got %q", stderr.String())
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called despite suggestion service error")
	}

	if mService.lastParams.Prompt != "fallback test" {
		t.Errorf("expected prompt 'fallback test', got %q", mService.lastParams.Prompt)
	}
}

func TestChatCommand_Execute_ShowTurnsLog(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Setup Mocks
	ml.ExpectedCalls = nil
	cfg := &config.Config{Mode: "assistant"}
	ml.On("Load", mock.Anything).Return(cfg, nil)
	mService.On("StreamTurnsLog", mock.Anything, cfg, &stdout).Return(nil).Run(func(args mock.Arguments) {
		out := args.Get(2).(io.Writer)
		_, _ = out.Write([]byte("turn 1: hello\nturn 2: world"))
	})

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
	}

	err := executeChatCommand(cmdCtx, []string{"-t"})
	require.NoError(t, err, "Execute should not fail")
	assert.Equal(t, "turn 1: hello\nturn 2: world", stdout.String())
	assert.False(t, mService.chatCalled, "expected chat service NOT to be called")
}

func TestChatCommand_Execute_ShowTurnsLog_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupMock   func(ms *mockChatService, ml *mockLoader)
		expectedErr string
	}{
		{
			name: "Config Load Failure",
			setupMock: func(ms *mockChatService, ml *mockLoader) {
				ml.On("Load", mock.Anything).Return(nil, errors.New("bad config"))
			},
			expectedErr: "error loading config",
		},
		{
			name: "StreamTurnsLog Failure",
			setupMock: func(ms *mockChatService, ml *mockLoader) {
				cfg := &config.Config{Mode: "assistant"}
				ml.On("Load", mock.Anything).Return(cfg, nil)
				ms.On("StreamTurnsLog", mock.Anything, cfg, mock.Anything).Return(errors.New("stream error"))
			},
			expectedErr: "stream error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &mockChatService{}
			ml := &mockLoader{}
			tt.setupMock(ms, ml)

			cmdCtx := &context{
				Loader:      ml,
				ChatService: ms,
				Stdout:      new(strings.Builder),
			}

			err := executeChatCommand(cmdCtx, []string{"-t"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)

			ms.AssertExpectations(t)
			ml.AssertExpectations(t)
		})
	}
}

func TestChatCommand_Execute_LastN_NoValue(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
	}

	err := executeChatCommand(cmdCtx, []string{"-l"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.LastN != 1 {
		t.Errorf("expected LastN 1 (default), got %d", mService.lastParams.LastN)
	}
}

func executeChatCommand(cmdCtx *context, args []string) error {
	root := &cobra.Command{}
	root.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")
	chatCmd := newChatCommand(cmdCtx)
	root.AddCommand(chatCmd)

	// We must mimic the logic in App.Run by sanitizing args
	fullArgs := append([]string{"tell-me-go", "chat"}, args...)
	sanitized := sanitizeArgs(fullArgs)
	// skip the binary name for SetArgs
	root.SetArgs(sanitized[1:])

	return root.ExecuteContext(stdctx.Background())
}
