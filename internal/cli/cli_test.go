// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestApp_Run_Version(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	version := "1.2.3"

	deps := defaultTestDeps()
	deps.Version = version
	deps.Stdout = stdout
	deps.Stderr = stderr
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	err = app.Run(stdctx.Background(), []string{"tell-me-go", "--version"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := stdout.String()
	expected := "tell-me-go version " + version + "\n"
	if output != expected {
		t.Errorf("expected output %q, got %q", expected, output)
	}
}

func TestApp_Run_UnknownFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	deps := defaultTestDeps()
	deps.Stdout = stdout
	deps.Stderr = stderr
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// unknown flag should fail.
	err = app.Run(stdctx.Background(), []string{"tell-me-go", "--unknown-flag"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestApp_Run_ContextCanceled(t *testing.T) {
	stderr := &bytes.Buffer{}

	mService := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
			return stdctx.Canceled
		},
	}

	deps := defaultTestDeps()
	deps.Stderr = stderr
	deps.ChatService = mService
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	cancel() // Cancel it immediately

	// Provide a prompt to avoid "empty prompt" error from capturer
	err = app.Run(ctx, []string{"tell-me-go", "test prompt"})
	if err != nil {
		t.Errorf("expected nil error on context cancellation, got %v", err)
	}

	// App.Run prints a newline to stderr on context cancellation
	if stderr.String() != "\n" {
		t.Errorf("expected newline on stderr, got %q", stderr.String())
	}
}

func TestApp_Run_CommandError(t *testing.T) {
	customErr := errors.New("custom error")
	mService := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
			return customErr
		},
	}

	deps := defaultTestDeps()
	deps.ChatService = mService
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Provide a prompt to avoid "empty prompt" error from capturer
	err = app.Run(stdctx.Background(), []string{"tell-me-go", "test prompt"})
	if !errors.Is(err, customErr) {
		t.Errorf("expected %v, got %v", customErr, err)
	}
}

func TestNew_MissingDependencies(t *testing.T) {
	tests := []struct {
		name  string
		setup func(deps *AppDependencies)
	}{
		{
			name:  "Missing Bootstrapper",
			setup: func(deps *AppDependencies) { deps.Bootstrapper = nil },
		},
		{
			name:  "Missing SM",
			setup: func(deps *AppDependencies) { deps.SM = nil },
		},
		{
			name:  "Missing ConfigLoader",
			setup: func(deps *AppDependencies) { deps.ConfigLoader = nil },
		},
		{
			name:  "Missing ChatService",
			setup: func(deps *AppDependencies) { deps.ChatService = nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := defaultTestDeps()
			tt.setup(&deps)

			_, err := New(deps, func(string) string { return "" })

			if !errors.Is(err, errMissingDependency) {
				t.Errorf("expected error %v for %s, got %v", errMissingDependency, tt.name, err)
			}
		})
	}
}

func defaultTestDeps() AppDependencies {
	return AppDependencies{
		Version:      "1.0.0",
		HomeDir:      ".",
		SM:           &mockSM{},
		Bootstrapper: &simpleMockBootstrapper{},
		ConfigLoader: &cliMockLoader{},
		ChatService:  &clitest.MockChatService{},
		Stdin:        strings.NewReader(""),
		Stdout:       new(bytes.Buffer),
		Stderr:       new(bytes.Buffer),
	}
}

type cliMockLoader struct{}

func (m *cliMockLoader) Load(path string) (*config.Config, error) {
	return &config.Config{}, nil
}

type simpleMockBootstrapper struct{}

func (m *simpleMockBootstrapper) BuildSessionDependencies(stdctx.Context, *config.Config, string, bool, agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(stdctx.Context) error, error) {
	return nil, nil, func(stdctx.Context) error { return nil }, nil
}
func (m *simpleMockBootstrapper) FinalizeSession(stdctx.Context, ports.HistoryManager, ports.SessionFinalizer, *config.Config) error {
	return nil
}
func (m *simpleMockBootstrapper) GetHistoryManager(stdctx.Context, *config.Config) (ports.HistoryManager, error) {
	return nil, nil
}
func (m *simpleMockBootstrapper) GetUnifiedHistoryProvider(stdctx.Context, *config.Config, ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	return nil, nil
}
func (m *simpleMockBootstrapper) GetSuggestionService(stdctx.Context, []string) (ports.SuggestionService, error) {
	return nil, nil
}

// Retain ONLY the methods that are strictly invoked during app.Run() setup
func (m *simpleMockBootstrapper) GetAgentFactory() ports.ChatterFactory     { return nil }
func (m *simpleMockBootstrapper) GetUIRenderer() ports.UIRenderer           { return nil }
func (m *simpleMockBootstrapper) GetHistoryRenderer() ports.HistoryRenderer { return nil }
func (m *simpleMockBootstrapper) GetHistoryBrowser() ports.HistoryBrowser   { return nil }
func (m *simpleMockBootstrapper) GetChatService() agent.ChatService         { return nil }

// mockSMWithTracking extends mockSM with Close() call tracking for shutdown tests.
type mockSMWithTracking struct {
	mockSM
	closeCalled bool
	closeErr    error
}

func (m *mockSMWithTracking) Close() error {
	m.closeCalled = true
	return m.closeErr
}

func TestApp_Run_SMCloseOnShutdown(t *testing.T) {
	sm := &mockSMWithTracking{}

	deps := defaultTestDeps()
	deps.SM = sm
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	err = app.Run(stdctx.Background(), []string{"tell-me-go", "--version"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !sm.closeCalled {
		t.Error("expected SM.Close() to be called on shutdown")
	}
}

func TestApp_Run_SMCloseError(t *testing.T) {
	sm := &mockSMWithTracking{
		closeErr: errors.New("close failed"),
	}

	deps := defaultTestDeps()
	deps.SM = sm
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Should not panic even when SM.Close() returns an error
	err = app.Run(stdctx.Background(), []string{"tell-me-go", "--version"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !sm.closeCalled {
		t.Error("expected SM.Close() to be called even when it returns an error")
	}
}

func TestApp_Run_SMCloseOnContextCancel(t *testing.T) {
	sm := &mockSMWithTracking{}

	mService := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
			return stdctx.Canceled
		},
	}

	deps := defaultTestDeps()
	deps.SM = sm
	deps.ChatService = mService
	app, err := New(deps, func(string) string { return "" })
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	cancel()

	err = app.Run(ctx, []string{"tell-me-go", "test prompt"})
	if err != nil {
		t.Errorf("expected nil error on context cancellation, got %v", err)
	}

	if !sm.closeCalled {
		t.Error("expected SM.Close() to be called on context cancellation")
	}
}

func TestNew_NilIOFallback(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(deps *AppDependencies)
		checkNil func(*App) bool // returns true if the field should be os.*
	}{
		{
			name:     "nil stdin falls back to os.Stdin",
			setup:    func(deps *AppDependencies) { deps.Stdin = nil },
			checkNil: func(a *App) bool { return a.Stdin == nil },
		},
		{
			name:     "nil stdout falls back to os.Stdout",
			setup:    func(deps *AppDependencies) { deps.Stdout = nil },
			checkNil: func(a *App) bool { return a.Stdout == nil },
		},
		{
			name:     "nil stderr falls back to os.Stderr",
			setup:    func(deps *AppDependencies) { deps.Stderr = nil },
			checkNil: func(a *App) bool { return a.Stderr == nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := defaultTestDeps()
			tt.setup(&deps)

			app, err := New(deps, func(string) string { return "" })
			if err != nil {
				t.Fatalf("New failed: %v", err)
			}

			if tt.checkNil(app) {
				t.Error("expected non-nil fallback to os.*, got nil")
			}
		})
	}
}

func TestSanitizeArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "no flags",
			args:     []string{"tell-me-go", "chat", "hello"},
			expected: []string{"tell-me-go", "chat", "hello"},
		},
		{
			name:     "short last flag with value",
			args:     []string{"tell-me-go", "-l", "10", "hello"},
			expected: []string{"tell-me-go", "-l=10", "hello"},
		},
		{
			name:     "short last flag without value",
			args:     []string{"tell-me-go", "-l", "hello"},
			expected: []string{"tell-me-go", "-l=1", "hello"},
		},
		{
			name:     "long last flag without value",
			args:     []string{"tell-me-go", "--last", "hello"},
			expected: []string{"tell-me-go", "--last=1", "hello"},
		},
		{
			name:     "short back flag without value",
			args:     []string{"tell-me-go", "-b", "hello"},
			expected: []string{"tell-me-go", "-b=1", "hello"},
		},
		{
			name:     "bare last flag",
			args:     []string{"tell-me-go", "-l"},
			expected: []string{"tell-me-go", "-l=1"},
		},
		{
			name:     "mixed flags - both are sanitized",
			args:     []string{"tell-me-go", "-l", "-b"},
			expected: []string{"tell-me-go", "-l=1", "-b=1"},
		},
		{
			name:     "last flag combined with another flag without space",
			args:     []string{"tell-me-go", "-l", "-r"},
			expected: []string{"tell-me-go", "-l=1", "-r"},
		},
		{
			name:     "last flag equal sign",
			args:     []string{"tell-me-go", "-l=5", "-r"},
			expected: []string{"tell-me-go", "-l=5", "-r"},
		},
		{
			name:     "single arg no sanitization needed",
			args:     []string{"tell-me-go"},
			expected: []string{"tell-me-go"},
		},
		{
			name:     "long back flag without value",
			args:     []string{"tell-me-go", "--back", "hello"},
			expected: []string{"tell-me-go", "--back=1", "hello"},
		},
		{
			name:     "long back flag with value",
			args:     []string{"tell-me-go", "--back", "3", "hello"},
			expected: []string{"tell-me-go", "--back=3", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeArgs(tt.args)
			if len(got) != len(tt.expected) {
				t.Errorf("expected len %d, got %d. Got: %v", len(tt.expected), len(got), got)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("at index %d: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}
