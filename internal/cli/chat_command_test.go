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
	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/config/configtest"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func setupMocks() (*clitest.MockBootstrapper, *configtest.MockConfigLoader, *clitest.MockChatService) {
	mb := &clitest.MockBootstrapper{}
	ml := &configtest.MockConfigLoader{
		LoadFunc: func(path string) (*config.Config, error) {
			return &config.Config{}, nil
		},
	}
	ms := &clitest.MockChatService{}
	mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
		return nil, nil
	}
	mb.GetSuggestionServiceFunc = func(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
		return &mockSuggestionService{}, nil
	}
	ms.ProcessMessageFunc = func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
		ms.ChatCalled = true
		ms.LastParams = cmd
		return nil
	}
	ms.GetLastUserMessageFunc = func(ctx stdctx.Context, hManager ports.HistoryManager) (string, int, error) {
		return "retry test", 1, nil
	}
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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.LastParams.Prompt != "hello" {
		t.Errorf("expected prompt 'hello', got %q", mService.LastParams.Prompt)
	}

	// ADR-021: Snapshot verifies bootstrapping orchestration.
	// Bootstrapping is a pre-CLI DI concern, so no methods are
	// expected to be called during command execution.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.LastParams.LastN != 5 {
		t.Errorf("expected LastN 5, got %d", mService.LastParams.LastN)
	}

	// ADR-021: Bootstrapping is a pre-CLI DI concern.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.LastParams.BackN != 2 {
		t.Errorf("expected BackN 2, got %d", mService.LastParams.BackN)
	}

	// ADR-021: Bootstrapping is a pre-CLI DI concern.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if !mService.LastParams.Retry {
		t.Error("expected Retry to be true")
	}

	// ADR-021: Bootstrapping is a pre-CLI DI concern.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
}

func TestChatCommand_Execute_Retry_Aborted(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Since retry logic is now in ChatService, we can test it by making the mock return an error or just checking if it was called with Retry: true.
	// For CLI tests, we just want to ensure that --retry flag is correctly parsed into agent.ChatCommand.
	mService.ProcessMessageFunc = func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
		mService.ChatCalled = true
		mService.LastParams = cmd
		return nil
	}

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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if !mService.LastParams.Retry {
		t.Error("expected Retry to be true")
	}

	// ADR-021: Bootstrapping is a pre-CLI DI concern.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
}

func TestChatCommand_Execute_SuggestionServiceError_Fallback(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()
	mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
		return nil, nil
	}
	mb.GetSuggestionServiceFunc = func(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
		return nil, errors.New("initialization failed")
	}

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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called despite suggestion service error")
	}

	if mService.LastParams.Prompt != "fallback test" {
		t.Errorf("expected prompt 'fallback test', got %q", mService.LastParams.Prompt)
	}

	// ADR-021: TUI path calls GetHistoryManager and GetSuggestionService
	// during capturer setup. Bootstrapping (BuildSessionDependencies) is
	// a pre-CLI DI concern.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
	if snap.GetHistoryManager != 1 {
		t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
	}
	if snap.GetSuggestionService != 1 {
		t.Errorf("GetSuggestionService: expected 1, got %d", snap.GetSuggestionService)
	}
}

func TestChatCommand_Execute_ShowTurnsLog(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Setup Mocks
	cfg := &config.Config{Mode: "assistant"}
	ml.LoadFunc = func(path string) (*config.Config, error) { return cfg, nil }
	mService.StreamTurnsLogFunc = func(ctx stdctx.Context, c *config.Config, out io.Writer) error {
		_, _ = out.Write([]byte("turn 1: hello\nturn 2: world"))
		return nil
	}

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
	assert.False(t, mService.ChatCalled, "expected chat service NOT to be called")

	// ADR-021: Turns-log path does not touch the bootstrapper.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
}

func TestChatCommand_Execute_ShowTurnsLog_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupMock   func() (*clitest.MockChatService, *configtest.MockConfigLoader)
		expectedErr string
	}{
		{
			name: "Config Load Failure",
			setupMock: func() (*clitest.MockChatService, *configtest.MockConfigLoader) {
				ml := &configtest.MockConfigLoader{LoadFunc: func(path string) (*config.Config, error) { return nil, errors.New("bad config") }}
				return &clitest.MockChatService{}, ml
			},
			expectedErr: "error loading config",
		},
		{
			name: "StreamTurnsLog Failure",
			setupMock: func() (*clitest.MockChatService, *configtest.MockConfigLoader) {
				cfg := &config.Config{Mode: "assistant"}
				ml := &configtest.MockConfigLoader{LoadFunc: func(path string) (*config.Config, error) { return cfg, nil }}
				ms := &clitest.MockChatService{StreamTurnsLogFunc: func(ctx stdctx.Context, c *config.Config, out io.Writer) error { return errors.New("stream error") }}
				return ms, ml
			},
			expectedErr: "stream error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, ml := tt.setupMock()

			cmdCtx := &context{
				Loader:      ml,
				ChatService: ms,
				Stdout:      new(strings.Builder),
			}

			err := executeChatCommand(cmdCtx, []string{"-t"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)

			snap := ms.Snapshot()
			if tt.name == "StreamTurnsLog Failure" {
				if snap.StreamTurnsLog != 1 {
					t.Errorf("expected StreamTurnsLog to be called once, got %d", snap.StreamTurnsLog)
				}
				snapML := ml.Snapshot()
				if snapML["Load"] != 1 {
					t.Errorf("expected Load to be called once, got %d", snapML["Load"])
				}
			} else {
				if snap.StreamTurnsLog != 0 {
					t.Errorf("expected StreamTurnsLog NOT to be called, got %d", snap.StreamTurnsLog)
				}
			}
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

	if !mService.ChatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.LastParams.LastN != 5 {
		t.Errorf("expected LastN 5, got %d", mService.LastParams.LastN)
	}

	// ADR-021: Bootstrapping is a pre-CLI DI concern.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
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
		setupMocks    func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService)
		expectedError string
	}{
		{
			name: "Config Load Failure",
			args: []string{"hello"},
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return nil, errors.New("config not found") }
			},
			expectedError: "error loading config",
		},
		{
			name: "TUI Capturer Cast Failure",
			args: []string{"--interactive"},
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				// BaseCapturer is NOT returned here since we don't inject TUI specific mocks.
				ml.LoadFunc = func(path string) (*config.Config, error) { return &config.Config{UseTUIPrompt: true}, nil }
				mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) { return nil, nil }
				mb.GetSuggestionServiceFunc = func(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
					return &mockSuggestionService{}, nil
				}
			},
			expectedError: "", // Test should fall back and not panic
		},
		{
			name: "Suggestion Service Error",
			args: []string{"--interactive"},
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return &config.Config{UseTUIPrompt: true}, nil }
				mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) { return nil, nil }
				mb.GetSuggestionServiceFunc = func(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
					return nil, errors.New("suggestion failure")
				}
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ml := &configtest.MockConfigLoader{}
			mb := &clitest.MockBootstrapper{}
			sm := &mockSM{}
			ms := &clitest.MockChatService{}
			ms.ProcessMessageFunc = func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
				return nil
			}
			tt.setupMocks(ml, mb, ms)

			var stdout, stderr strings.Builder
			cmdCtx := &context{
				Version:      "1.0.0",
				Stdin:        strings.NewReader("test\n"),
				Stdout:       &stdout,
				Stderr:       &stderr,
				SM:           sm,
				ChatService:  ms,
				Bootstrapper: mb,
				Loader:       ml,
				HomeDir:      t.TempDir(),
			}

			err := executeChatCommand(cmdCtx, tt.args)
			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
				// ADR-021: Config load fails before bootstrapper is reached.
				snap := mb.Snapshot()
				if snap.BuildSessionDependencies != 0 {
					t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
				}
			} else {
				require.NoError(t, err)
				// ADR-021: TUI path calls GetHistoryManager and GetSuggestionService
				// during capturer setup.
				snap := mb.Snapshot()
				if snap.BuildSessionDependencies != 0 {
					t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
				}
				if snap.GetHistoryManager != 1 {
					t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
				}
				if snap.GetSuggestionService != 1 {
					t.Errorf("GetSuggestionService: expected 1, got %d", snap.GetSuggestionService)
				}
			}
		})
	}
}

func TestChatCommand_Execute_Diagnostic(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Setup Mocks
	cfg := &config.Config{Mode: "assistant"}
	ml.LoadFunc = func(path string) (*config.Config, error) { return cfg, nil }
	mService.RunDiagnosticsFunc = func(ctx stdctx.Context, c *config.Config, configPath string, jsonOutput bool) error { return nil }

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
	assert.False(t, mService.ChatCalled, "expected chat service NOT to be called")
	snap := mService.Snapshot()
	if snap.RunDiagnostics != 1 {
		t.Errorf("expected RunDiagnostics to be called once, got %d", snap.RunDiagnostics)
	}

	// ADR-021: Diagnostic path does not touch the bootstrapper.
	bootSnap := mb.Snapshot()
	if bootSnap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", bootSnap.BuildSessionDependencies)
	}
}

func TestChatCommand_Execute_Diagnostic_JSON(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	// Setup Mocks
	cfg := &config.Config{Mode: "assistant"}
	ml.LoadFunc = func(path string) (*config.Config, error) { return cfg, nil }
	var gotJSON bool
	mService.RunDiagnosticsFunc = func(ctx stdctx.Context, c *config.Config, configPath string, jsonOutput bool) error {
		gotJSON = jsonOutput
		return nil
	}

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
	if !gotJSON {
		t.Error("expected jsonOutput to be true")
	}

	// ADR-021: Diagnostic path does not touch the bootstrapper.
	bootSnap := mb.Snapshot()
	if bootSnap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", bootSnap.BuildSessionDependencies)
	}
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

	// ADR-021: TUI path calls GetHistoryManager and GetSuggestionService
	// during capturer setup.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
	if snap.GetHistoryManager != 1 {
		t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
	}
	if snap.GetSuggestionService != 1 {
		t.Errorf("GetSuggestionService: expected 1, got %d", snap.GetSuggestionService)
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

	// ADR-021: Non-TUI path does not touch the bootstrapper.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
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

	// ADR-021: Non-TUI path does not touch the bootstrapper.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
}

func TestChatCommand_CleanupErrorPropagation(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, mService := setupMocks()

	closeErr := errors.New("capturer close failed")
	mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
		return nil, nil
	}
	mb.GetSuggestionServiceFunc = func(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
		return &mockSuggestionService{closeErr: closeErr}, nil
	}

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader("test prompt\n"),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		ChatService:  mService,
		Bootstrapper: mb,
		Loader:       ml,
		HomeDir:      t.TempDir(),
	}

	err := executeChatCommand(cmdCtx, []string{"--interactive"})
	require.NoError(t, err, "chat command should not fail on cleanup error")
	require.Contains(t, stderr.String(), "Warning: failed to close capturer")
	require.Contains(t, stderr.String(), "capturer close failed")

	// ADR-021: TUI path calls GetHistoryManager and GetSuggestionService
	// during capturer setup, even when cleanup later fails.
	snap := mb.Snapshot()
	if snap.BuildSessionDependencies != 0 {
		t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
	}
	if snap.GetHistoryManager != 1 {
		t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
	}
	if snap.GetSuggestionService != 1 {
		t.Errorf("GetSuggestionService: expected 1, got %d", snap.GetSuggestionService)
	}
}

// TestChatCommand_SetupCapturer_CleanupError verifies that when the
// capturerOverride path is taken, the cleanup function propagates
// Close errors and writes a warning to stderr.
//
// Coverage note: setupCapturer is at 75% (architectural ceiling without
// DI for ui.NewCapturer). The uncovered lines are:
//  1. The !ok type-assertion fallback (lines 277-279) — defensive guard
//     identical to buildCapturer's BaseCapturer fallback.
//  2. The non-override cleanup error path (lines 282-285) — requires
//     a mock capturer from ui.NewCapturer which is not injectable.
//
// Both require refactoring ui.NewCapturer to accept an interface factory.
func TestChatCommand_SetupCapturer_CleanupError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("capturer close exploded")
	mockCap := &mockCapturerInteractor{closeFn: func(ctx stdctx.Context) error { return closeErr }}

	var stderr strings.Builder
	c := &chatCommand{
		Stderr:           &stderr,
		capturerOverride: mockCap,
	}

	capturer, cleanup, err := c.setupCapturer()
	require.NoError(t, err)
	require.NotNil(t, capturer)
	require.NotNil(t, cleanup)

	// Trigger the cleanup — should propagate error AND write warning
	err = cleanup(stdctx.Background())
	require.ErrorIs(t, err, closeErr)
	require.Contains(t, stderr.String(), "Warning: failed to close capturer")
	require.Contains(t, stderr.String(), "capturer close exploded")
}

// TestChatCommand_SetupCapturer_OverrideCloseSuccess verifies that when
// capturerOverride is set and Close succeeds, setupCapturer's cleanup
// returns nil and writes nothing to stderr.
func TestChatCommand_SetupCapturer_OverrideCloseSuccess(t *testing.T) {
	t.Parallel()

	mockCap := &mockCapturerInteractor{} // closeFn nil → Close returns nil

	var stderr strings.Builder
	c := &chatCommand{
		Stderr:           &stderr,
		capturerOverride: mockCap,
	}

	capturer, cleanup, err := c.setupCapturer()
	require.NoError(t, err)
	require.NotNil(t, capturer)
	require.NotNil(t, cleanup)

	err = cleanup(stdctx.Background())
	require.NoError(t, err, "cleanup should not error when Close succeeds")
	require.Empty(t, stderr.String(), "stderr should be empty when Close succeeds")
}

// TestChatCommand_SetupCapturer_NonOverrideCloseError verifies that when
// capturerOverride is nil and the factory returns a capturer whose Close
// fails, the cleanup propagates the error and writes a warning to stderr.
func TestChatCommand_SetupCapturer_NonOverrideCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("non-override close exploded")
	mockCap := &mockCapturerInteractor{
		closeFn: func(ctx stdctx.Context) error { return closeErr },
	}

	var stderr strings.Builder
	c := &chatCommand{
		Stderr: &stderr,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return mockCap
		},
	}

	capturer, cleanup, err := c.setupCapturer()
	require.NoError(t, err, "setupCapturer should not error when factory returns valid capturer")
	require.NotNil(t, capturer)
	require.NotNil(t, cleanup)

	err = cleanup(stdctx.Background())
	require.ErrorIs(t, err, closeErr, "cleanup should propagate Close error from factory-provided capturer")
	require.Contains(t, stderr.String(), "Warning: failed to close capturer")
	require.Contains(t, stderr.String(), "non-override close exploded")
}

func TestChatCommand_PrepareCaptureOptions_RawFlag(t *testing.T) {
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

	err := executeChatCommand(cmdCtx, []string{"--raw", "hello"})
	require.NoError(t, err)
	require.True(t, mService.ChatCalled)
	require.True(t, mService.LastParams.RawOutput, "expected RawOutput to be true when --raw flag is set")

	// Also test short flag -r
	err = executeChatCommand(cmdCtx, []string{"-r", "hello"})
	require.NoError(t, err)
	require.True(t, mService.LastParams.RawOutput)
}

// TestChatCommand_GetCapturer_OverrideCloseError verifies that when
// capturerOverride is set, getCapturer returns a cleanup function that
// propagates Close errors and writes a warning to stderr.
func TestChatCommand_GetCapturer_OverrideCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("getCapturer close exploded")
	mockCap := &mockCapturerInteractor{
		closeFn: func(ctx stdctx.Context) error { return closeErr },
	}

	var stderr strings.Builder
	c := &chatCommand{
		Stderr:           &stderr,
		capturerOverride: mockCap,
	}

	capturer, cleanup, err := c.getCapturer(stdctx.Background(), nil, nil)
	require.NoError(t, err, "getCapturer should not error when override is set")
	require.NotNil(t, capturer, "expected non-nil capturer from getCapturer override path")
	require.NotNil(t, cleanup, "expected non-nil cleanup from getCapturer override path")

	// Trigger the cleanup — should propagate error AND write warning
	err = cleanup(stdctx.Background())
	require.ErrorIs(t, err, closeErr, "cleanup should propagate the Close error")
	require.Contains(t, stderr.String(), "Warning: failed to close capturer")
	require.Contains(t, stderr.String(), "getCapturer close exploded")
}

// TestChatCommand_GetCapturer_OverrideCloseSuccess verifies that when
// capturerOverride is set and Close succeeds, the cleanup returns nil
// and writes nothing to stderr.
func TestChatCommand_GetCapturer_OverrideCloseSuccess(t *testing.T) {
	t.Parallel()

	mockCap := &mockCapturerInteractor{} // closeFn nil → Close returns nil

	var stderr strings.Builder
	c := &chatCommand{
		Stderr:           &stderr,
		capturerOverride: mockCap,
	}

	capturer, cleanup, err := c.getCapturer(stdctx.Background(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, capturer)
	require.NotNil(t, cleanup)

	err = cleanup(stdctx.Background())
	require.NoError(t, err, "cleanup should not error when Close succeeds")
	require.Empty(t, stderr.String(), "stderr should be empty when Close succeeds")
}

// TestChatCommand_SetupCapturer_NonCapturerInteractor verifies that
// when the capturerFactory returns a value implementing UserInteractor
// but NOT agent.CapturerInteractor, setupCapturer returns an error.
func TestChatCommand_SetupCapturer_NonCapturerInteractor(t *testing.T) {
	t.Parallel()

	// stubInteractor implements domain_security.UserInteractor but
	// lacks CapturePrompt, IsTTY, and Close — so it does NOT satisfy
	// agent.CapturerInteractor, triggering the !ok assertion.
	nonCapturer := &stubInteractor{id: 1}

	var stderr strings.Builder
	c := &chatCommand{
		Stderr: &stderr,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return nonCapturer
		},
	}

	capturer, cleanup, err := c.setupCapturer()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ui.NewCapturer did not return an agent.CapturerInteractor")
	require.Nil(t, capturer)
	require.Nil(t, cleanup)
}

// TestChatCommand_BuildCapturer_NonTUI_SetupCapturerError verifies that
// when tuiPrompt is false and setupCapturer returns an error (via
// capturerFactory returning a non-CapturerInteractor), buildCapturer
// propagates the error as (nil, nil, err).
func TestChatCommand_BuildCapturer_NonTUI_SetupCapturerError(t *testing.T) {
	t.Parallel()

	nonCapturer := &stubInteractor{id: 3}

	c := &chatCommand{
		Stdin:  strings.NewReader(""),
		Stdout: new(strings.Builder),
		Stderr: new(strings.Builder),
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return nonCapturer
		},
	}

	opts := &cliOptions{tuiPrompt: false} // non-TUI path
	capturer, cleanup, err := c.buildCapturer(stdctx.Background(), nil, opts)

	require.Error(t, err)
	require.Contains(t, err.Error(), "ui.NewCapturer did not return an agent.CapturerInteractor")
	require.Nil(t, capturer)
	require.Nil(t, cleanup)
}

// TestChatCommand_ExecuteChat_SetupSessionError verifies that when
// setupChatSession → getCapturer → buildCapturer → setupCapturer fails
// (because the factory returns a non-CapturerInteractor), executeChat
// returns the error without calling processChatRequest.
func TestChatCommand_ExecuteChat_SetupSessionError(t *testing.T) {
	t.Parallel()

	nonCapturer := &stubInteractor{id: 4}

	var stdout, stderr strings.Builder
	ml := &configtest.MockConfigLoader{
		LoadFunc: func(path string) (*config.Config, error) { return &config.Config{}, nil },
	}

	c := &chatCommand{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Loader: ml,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return nonCapturer
		},
	}

	opts := &cliOptions{} // tuiPrompt defaults to false → non-TUI path
	err := c.executeChat(stdctx.Background(), opts, []string{"hello"})

	require.Error(t, err)
	require.Contains(t, err.Error(), "ui.NewCapturer did not return an agent.CapturerInteractor")
}
