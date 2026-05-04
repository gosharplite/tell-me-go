// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockChatService struct {
	mock.Mock
	chatCalled bool
	lastParams agent.ChatCommand
}

func (m *mockChatService) ProcessMessage(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
	m.chatCalled = true
	m.lastParams = cmd
	args := m.Called(ctx, cfg, cmd, capturer)
	return args.Error(0)
}

func (m *mockChatService) GetLastUserMessage(ctx stdctx.Context, hManager ports.HistoryManager) (string, int, error) {
	args := m.Called(ctx, hManager)
	return args.String(0), args.Int(1), args.Error(2)
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

func (m *mockChatService) RunDiagnostics(ctx stdctx.Context, cfg *config.Config, configPath string, jsonOutput bool) error {
	args := m.Called(ctx, cfg, configPath, jsonOutput)
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

type chatMockLoader struct {
	mock.Mock
}

func (m *chatMockLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *chatMockLoader) Watch(ctx stdctx.Context, path string) (<-chan *config.Config, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(<-chan *config.Config), args.Error(1)
}

type mockSuggestionService struct {
	closeErr error
}

func (m *mockSuggestionService) GetSuggestions(ctx stdctx.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (m *mockSuggestionService) RecordPrompt(ctx stdctx.Context, prompt string) error { return nil }
func (m *mockSuggestionService) Close(ctx stdctx.Context) error                       { return m.closeErr }

type mockSM struct {
	domain_security.Manager
}

func (m *mockSM) TerminalLock()   {}
func (m *mockSM) TerminalUnlock() {}
func (m *mockSM) Close() error    { return nil }

func setupMocks() (*mockBootstrapper, *chatMockLoader, *mockChatService) {
	mb := &mockBootstrapper{}
	ml := &chatMockLoader{}
	ms := &mockChatService{}
	ml.On("Load", mock.Anything).Return(&config.Config{}, nil).Maybe()
	mb.On("GetHistoryManager", mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	mb.On("GetSuggestionService", mock.Anything, mock.Anything).Return(&mockSuggestionService{}, nil).Maybe()
	ms.On("ProcessMessage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	ms.On("GetLastUserMessage", mock.Anything, mock.Anything).Return("retry test", 1, nil).Maybe()
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
		Stdin:        strings.NewReader(""),
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

	if !mService.lastParams.Retry {
		t.Error("expected Retry to be true")
	}
}

func TestChatCommand_Execute_Retry_Aborted(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Since retry logic is now in ChatService, we can test it by making the mock return an error or just checking if it was called with Retry: true.
	// For CLI tests, we just want to ensure that --retry flag is correctly parsed into agent.ChatCommand.
	mService.ExpectedCalls = nil
	mService.On("ProcessMessage", mock.Anything, mock.Anything, mock.MatchedBy(func(cmd agent.ChatCommand) bool {
		return cmd.Retry
	}), mock.Anything).Return(nil)

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

	err := executeChatCommand(cmdCtx, []string{"--retry"})
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if !mService.lastParams.Retry {
		t.Error("expected Retry to be true")
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

	err := executeChatCommand(cmdCtx, []string{"--interactive"})
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
		setupMock   func(ms *mockChatService, ml *chatMockLoader)
		expectedErr string
	}{
		{
			name: "Config Load Failure",
			setupMock: func(ms *mockChatService, ml *chatMockLoader) {
				ml.On("Load", mock.Anything).Return(nil, errors.New("bad config"))
			},
			expectedErr: "error loading config",
		},
		{
			name: "StreamTurnsLog Failure",
			setupMock: func(ms *mockChatService, ml *chatMockLoader) {
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
			ml := &chatMockLoader{}
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

func TestChatCommand_Execute_LastN_Positional(t *testing.T) {
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

	err := executeChatCommand(cmdCtx, []string{"-l", "5", "hello"})
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

func executeChatCommand(cmdCtx *context, args []string) error {
	root := &cobra.Command{}
	root.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")
	chatCmd := newChatCommand(cmdCtx, nil)
	root.AddCommand(chatCmd)

	// Since we removed manual routing, we can just use the natural Cobra behavior.
	// For these tests, we'll mimic the Root command's RunE behavior by setting it up.
	root.RunE = chatCmd.RunE
	root.Args = cobra.ArbitraryArgs
	chatCmd.Flags().VisitAll(func(f *pflag.Flag) {
		root.Flags().AddFlag(f)
	})

	// Use sanitizeArgs similarly to App.Run
	sanitized := sanitizeArgs(append([]string{"dummy"}, args...))
	root.SetArgs(sanitized[1:])

	return root.ExecuteContext(stdctx.Background())
}
func TestChatCommand_Execute_Errors(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		setupMocks    func(ml *chatMockLoader, mb *mockBootstrapper, ms *mockChatService)
		expectedError string
	}{
		{
			name: "Config Load Failure",
			args: []string{"hello"},
			setupMocks: func(ml *chatMockLoader, mb *mockBootstrapper, ms *mockChatService) {
				ml.On("Load", mock.Anything).Return(nil, errors.New("config not found"))
			},
			expectedError: "error loading config",
		},
		{
			name: "TUI Capturer Cast Failure",
			args: []string{"--interactive"},
			setupMocks: func(ml *chatMockLoader, mb *mockBootstrapper, ms *mockChatService) {
				// BaseCapturer is NOT returned here since we don't inject TUI specific mocks.
				ml.On("Load", mock.Anything).Return(&config.Config{UseTUIPrompt: true}, nil)
				mb.On("GetHistoryManager", mock.Anything, mock.Anything).Return(nil, nil)
				mb.On("GetSuggestionService", mock.Anything, mock.Anything).Return(&mockSuggestionService{}, nil)
			},
			expectedError: "", // Test should fall back and not panic
		},
		{
			name: "Suggestion Service Error",
			args: []string{"--interactive"},
			setupMocks: func(ml *chatMockLoader, mb *mockBootstrapper, ms *mockChatService) {
				ml.On("Load", mock.Anything).Return(&config.Config{UseTUIPrompt: true}, nil)
				mb.On("GetHistoryManager", mock.Anything, mock.Anything).Return(nil, nil)
				mb.On("GetSuggestionService", mock.Anything, mock.Anything).Return(nil, errors.New("suggestion failure"))
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ml := &chatMockLoader{}
			mb := &mockBootstrapper{}
			sm := &mockSM{}
			ms := &mockChatService{}
			ms.On("ProcessMessage", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			tt.setupMocks(ml, mb, ms)

			var stdout, stderr strings.Builder
			cmdCtx := &context{
				Version:      "1.0.0",
				Stdin:        strings.NewReader(""),
				Stdout:       &stdout,
				Stderr:       &stderr,
				SM:           sm,
				ChatService:  ms,
				Bootstrapper: mb,
				Loader:       ml,
			}

			err := executeChatCommand(cmdCtx, tt.args)
			require.ErrorContains(t, err, tt.expectedError)
		})
	}
}

func TestChatCommand_Execute_Diagnostic(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Setup Mocks
	ml.ExpectedCalls = nil
	cfg := &config.Config{Mode: "assistant"}
	ml.On("Load", mock.Anything).Return(cfg, nil)
	mService.On("RunDiagnostics", mock.Anything, cfg, mock.Anything, false).Return(nil)

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

	err := executeChatCommand(cmdCtx, []string{"--diagnostic"})
	require.NoError(t, err, "Execute should not fail")
	assert.False(t, mService.chatCalled, "expected chat service NOT to be called")
	mService.AssertExpectations(t)
}

func TestChatCommand_Execute_Diagnostic_JSON(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Setup Mocks
	ml.ExpectedCalls = nil
	cfg := &config.Config{Mode: "assistant"}
	ml.On("Load", mock.Anything).Return(cfg, nil)
	mService.On("RunDiagnostics", mock.Anything, cfg, mock.Anything, true).Return(nil)

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

	err := executeChatCommand(cmdCtx, []string{"--diagnostic", "--json"})
	require.NoError(t, err, "Execute should not fail")
	mService.AssertExpectations(t)
}

func TestChatCommand_Execute_TUIPrompt_PopulatesInteractorRef(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	ref := NewInteractorRef()
	if got := ref.Get(); got != nil {
		t.Fatalf("expected empty InteractorRef before run, got %T", got)
	}

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader("hello\n"),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
		HomeDir:      t.TempDir(),
		Interactor:   ref,
	}

	if err := executeChatCommand(cmdCtx, []string{"--interactive"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := ref.Get()
	if got == nil {
		t.Fatal("expected InteractorRef to be populated after --interactive run, got nil")
	}
}

func TestChatCommand_Execute_NonTUI_PopulatesInteractorRef(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	ref := NewInteractorRef()

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
		Interactor:   ref,
	}

	if err := executeChatCommand(cmdCtx, []string{"hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ref.Get() == nil {
		t.Fatal("expected InteractorRef to be populated after non-TUI run, got nil")
	}
}

func TestChatCommand_NilInteractorRef_DoesNotPanic(t *testing.T) {
	t.Parallel()

	// Passing a nil *InteractorRef must be safe — the InteractorRef.Set
	// method nil-guards internally so commands run without a wiring cell
	// (e.g., in narrowly-scoped tests).
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
		Interactor:   nil, // explicit
	}

	if err := executeChatCommand(cmdCtx, []string{"hello"}); err != nil {
		t.Fatalf("unexpected error with nil InteractorRef: %v", err)
	}
}
