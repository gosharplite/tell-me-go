// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockFailingFS struct {
	infra_persistence.FileSystem
	mkdirErr error
	openErr  error
	statErr  error
}

func (m *mockFailingFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return m.mkdirErr
}

func (m *mockFailingFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	return nil, errors.New("not implemented")
}

func (m *mockFailingFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if m.statErr != nil {
		return nil, m.statErr
	}
	return nil, os.ErrNotExist
}

func (m *mockFailingFS) CreateTemp(ctx context.Context, dir, pattern string) (infra_persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	return nil, errors.New("not implemented")
}

func (m *mockFailingFS) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	return nil, errors.New("not implemented")
}

// mockUnifiedHistoryProvider is a hand-rolled test double for ports.UnifiedHistoryProvider.
type mockUnifiedHistoryProvider struct {
	GetHistoryStreamFunc  func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error)
	getHistoryStreamCalls int
}

func (m *mockUnifiedHistoryProvider) GetHistoryStream(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
	m.getHistoryStreamCalls++
	if m.GetHistoryStreamFunc != nil {
		return m.GetHistoryStreamFunc(ctx, limit, cursor)
	}
	return nil, "", nil
}

// mockHistoryManagerFull is a hand-rolled test double for ports.HistoryManager.
type mockHistoryManagerFull struct {
	GetWindowFunc           func(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error)
	GetTotalEntriesFunc     func() int
	GetLastUserMessageFunc  func(ctx context.Context) (string, int, error)
	GetResolverFunc         func() llm.AssetResolver
	SetContentsFunc         func(ctx context.Context, contents []*llm.Content) error
	AddContentFunc          func(ctx context.Context, content *llm.Content) error
	AppendPartsFunc         func(ctx context.Context, index int, parts []*llm.Part) error
	SaveFunc                func(ctx context.Context) error
	SyncFunc                func(ctx context.Context) error
	ArchiveFunc             func(ctx context.Context, contents []*llm.Content) error
	SetPinnedFunc           func(ctx context.Context, turnIndex int, pinned bool) error
	GetFilePathFunc         func() string
	RollbackTurnsFunc       func(ctx context.Context, turns int) (int, int, int, error)
	getWindowCalls          int
	getTotalEntriesCalls    int
	getLastUserMessageCalls int
	getResolverCalls        int
	setContentsCalls        int
	addContentCalls         int
	appendPartsCalls        int
	saveCalls               int
	syncCalls               int
	archiveCalls            int
	setPinnedCalls          int
	getFilePathCalls        int
	rollbackTurnsCalls      int
}

func (m *mockHistoryManagerFull) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	m.getWindowCalls++
	if m.GetWindowFunc != nil {
		return m.GetWindowFunc(ctx, startIdx, endIdx)
	}
	return nil, nil
}

func (m *mockHistoryManagerFull) GetTotalEntries() int {
	m.getTotalEntriesCalls++
	if m.GetTotalEntriesFunc != nil {
		return m.GetTotalEntriesFunc()
	}
	return 0
}

func (m *mockHistoryManagerFull) GetLastUserMessage(ctx context.Context) (string, int, error) {
	m.getLastUserMessageCalls++
	if m.GetLastUserMessageFunc != nil {
		return m.GetLastUserMessageFunc(ctx)
	}
	return "", 0, nil
}

func (m *mockHistoryManagerFull) GetResolver() llm.AssetResolver {
	m.getResolverCalls++
	if m.GetResolverFunc != nil {
		return m.GetResolverFunc()
	}
	return nil
}

func (m *mockHistoryManagerFull) SetContents(ctx context.Context, contents []*llm.Content) error {
	m.setContentsCalls++
	if m.SetContentsFunc != nil {
		return m.SetContentsFunc(ctx, contents)
	}
	return nil
}

func (m *mockHistoryManagerFull) AddContent(ctx context.Context, content *llm.Content) error {
	m.addContentCalls++
	if m.AddContentFunc != nil {
		return m.AddContentFunc(ctx, content)
	}
	return nil
}

func (m *mockHistoryManagerFull) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	m.appendPartsCalls++
	if m.AppendPartsFunc != nil {
		return m.AppendPartsFunc(ctx, index, parts)
	}
	return nil
}

func (m *mockHistoryManagerFull) Save(ctx context.Context) error {
	m.saveCalls++
	if m.SaveFunc != nil {
		return m.SaveFunc(ctx)
	}
	return nil
}

func (m *mockHistoryManagerFull) Sync(ctx context.Context) error {
	m.syncCalls++
	if m.SyncFunc != nil {
		return m.SyncFunc(ctx)
	}
	return nil
}

func (m *mockHistoryManagerFull) Archive(ctx context.Context, contents []*llm.Content) error {
	m.archiveCalls++
	if m.ArchiveFunc != nil {
		return m.ArchiveFunc(ctx, contents)
	}
	return nil
}

func (m *mockHistoryManagerFull) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	m.setPinnedCalls++
	if m.SetPinnedFunc != nil {
		return m.SetPinnedFunc(ctx, turnIndex, pinned)
	}
	return nil
}

func (m *mockHistoryManagerFull) GetFilePath() string {
	m.getFilePathCalls++
	if m.GetFilePathFunc != nil {
		return m.GetFilePathFunc()
	}
	return ""
}

func (m *mockHistoryManagerFull) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	m.rollbackTurnsCalls++
	if m.RollbackTurnsFunc != nil {
		return m.RollbackTurnsFunc(ctx, turns)
	}
	return 0, 0, 0, nil
}

func TestGetHistoryManager_FailurePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("disk failure")

	tests := []struct {
		name    string
		fs      *mockFailingFS
		wantErr error
	}{
		{
			name: "DirectoryCreationFailure",
			fs: &mockFailingFS{
				mkdirErr: simulatedErr,
			},
			wantErr: errInfraInit,
		},
		{
			name: "BuildHistoryManagerFailure",
			fs: &mockFailingFS{
				openErr: simulatedErr, // simulate error during history manager build
				statErr: simulatedErr, // force load to fail instead of just 'not found'
			},
			wantErr: errInfraInit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)
			bcfg := DefaultBootstrapperConfig()
			bcfg.HomeDir = tempDir
			bcfg.SM = sm
			bcfg.Version = "1.0.0"
			bcfg.Stdout = io.Discard
			bcfg.Stderr = io.Discard
			bcfg.FileSystem = tt.fs
			b := NewBootstrapper(bcfg)
			testCfg := &config.Config{Mode: "assistant"}

			hManager, err := b.GetHistoryManager(ctx, testCfg)
			assert.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, hManager)
		})
	}
}

func TestBuildTelemetry_Fallback(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("failed to open file")

	fs := &mockFailingFS{
		openErr: simulatedErr,
	}

	sm := new(mockConfigurableSecurityManager)
	factory := newTelemetryFactory(tempDir, fs, sm, nil)

	paths := &persistence.Paths{
		TurnsLogPath: "turns.log",
	}
	cfg := &config.Config{Model: "test-model"}

	dummyCleanup := func(ctx context.Context) error { return nil }
	pricingData, tracker, turnsLogger, cleanup := factory.BuildTelemetry(ctx, paths, cfg, map[string]pricing.ModelPricing{}, dummyCleanup)

	assert.NotNil(t, pricingData)
	assert.NotNil(t, tracker)
	assert.NotNil(t, turnsLogger)
	assert.NotNil(t, cleanup)

	// Verify it's a No-Op turns logger
	_, ok := turnsLogger.(*ports.NoOpTurnsLogger)
	assert.True(t, ok, "Expected NoOpTurnsLogger on initialization failure")
}

func TestBuildSession_FailurePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("io error")

	tests := []struct {
		name    string
		setup   func(f *defaultSessionFactory)
		wantErr error
	}{
		{
			name: "EnsureDirectoriesFailure",
			setup: func(f *defaultSessionFactory) {
				f.setupSecurityFunc = f.setupSecurity
				f.FileSystem = &mockFailingFS{mkdirErr: simulatedErr}
			},
			wantErr: errInfraInit,
		},
		{
			name: "SessionStateInitializationFailure",
			setup: func(f *defaultSessionFactory) {
				f.setupSecurityFunc = f.setupSecurity
				f.FileSystem = &infra_persistence.OSFileSystem{}
				f.NewSessionState = func(ctx context.Context, modeDir string) (ports.SessionProvider, error) {
					return nil, simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
		{
			name: "SetupSecurityFailure",
			setup: func(f *defaultSessionFactory) {
				f.FileSystem = &infra_persistence.OSFileSystem{}
				f.setupSecurityFunc = func(paths *persistence.Paths, configPath string) error {
					return simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)

			// Initialize with a real but dummy filesystem to avoid nil panics in EnsureDirectories
			factory := newSessionFactory(tempDir, &infra_persistence.OSFileSystem{}, sm, io.Discard, io.Discard, nil, nil, nil).(*defaultSessionFactory)
			if tt.setup != nil {
				tt.setup(factory)
			}

			cfg := &config.Config{Mode: "assistant"}
			sp, paths, cleanup, err := factory.BuildSession(ctx, cfg, "config.yaml", false, nil)

			assert.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sp)
			assert.Nil(t, paths)
			assert.Nil(t, cleanup)
		})
	}
}

func TestBuildRegistry_FailurePaths(t *testing.T) {
	tempDir := t.TempDir()
	simulatedErr := errors.New("registration error")

	tests := []struct {
		name    string
		setup   func(f *defaultToolchainFactory, sm *mockConfigurableSecurityManager)
		wantErr error
	}{
		{
			name: "RegisterAllToolsFailure",
			setup: func(f *defaultToolchainFactory, sm *mockConfigurableSecurityManager) {
				f.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error {
					return simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
		{
			name: "RegisterMetricsFailure",
			setup: func(f *defaultToolchainFactory, sm *mockConfigurableSecurityManager) {
				f.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error { return nil }
				f.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile, model, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
					return simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
		{
			name: "RegisterPolicyToolsFailure",
			setup: func(f *defaultToolchainFactory, sm *mockConfigurableSecurityManager) {
				f.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error { return nil }
				f.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile, model, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
					return nil
				}
				// Policy tools registration is done via SM
				sm.RegisterPolicyToolsFunc = func(r tools.Registry, kv ports.KVStore) error {
					return simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)

			factory := newToolchainFactory(tempDir, nil, sm, nil, nil, nil).(*defaultToolchainFactory)
			if tt.setup != nil {
				tt.setup(factory, sm)
			}

			mockSP := &mockSessionProvider{
				GetSettingsFunc: func() ports.KVStore {
					return &mockKVStore{}
				},
			}

			params := toolchainParams{
				Paths:           &persistence.Paths{},
				SessionProvider: mockSP,
			}

			reg, err := factory.BuildRegistry(params)
			assert.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, reg)
		})
	}
}

func TestGetUnifiedHistoryProvider_FailurePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("mkdir failure")

	fs := &mockFailingFS{
		mkdirErr: simulatedErr,
	}

	sm := new(mockConfigurableSecurityManager)
	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.FileSystem = fs
	b := NewBootstrapper(bcfg)
	testCfg := &config.Config{Mode: "assistant"}

	hManager := history.NewManager(nil, "history.jsonl", "archive.jsonl")
	provider, err := b.GetUnifiedHistoryProvider(ctx, testCfg, hManager)

	assert.Error(t, err)
	assert.ErrorIs(t, err, errInfraInit)
	assert.Nil(t, provider)
}
func TestGetSuggestionService_Fallback(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Create an invalid directory structure to force NewGlobalPromptTracker to fail.
	// NewGlobalPromptTracker tries to create a file in homeDir/.tellmego/prompts.jsonl
	// If .tellmego is a file, it will fail.
	err := os.WriteFile(filepath.Join(tempDir, ".tellmego"), []byte(""), 0644)
	assert.NoError(t, err)

	sm := new(mockConfigurableSecurityManager)
	fs := &infra_persistence.OSFileSystem{}
	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.FileSystem = fs
	b := NewBootstrapper(bcfg)

	svc, err := b.GetSuggestionService(ctx, []string{"test"})
	assert.NoError(t, err)
	assert.NotNil(t, svc)
}

// failingCloser is an io.Closer that returns a configurable error.
type failingCloser struct{ err error }

func (c *failingCloser) Close() error { return c.err }

// stubProgramRunner is a programRunner that immediately succeeds.
type stubProgramRunner struct{}

func (r *stubProgramRunner) Run() (tea.Model, error) { return nil, nil }

// failingProgramRunner is a programRunner that returns a configurable error.
type failingProgramRunner struct{ err error }

func (r *failingProgramRunner) Run() (tea.Model, error) { return nil, r.err }

// quitModel is a minimal bubbletea Model that immediately quits.
type quitModel struct{}

func (m quitModel) Init() tea.Cmd { return tea.Quit }
func (m quitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, nil
}
func (m quitModel) View() string { return "" }

func TestTUIHistoryBrowser_Browse_LoggerCloseError(t *testing.T) {
	ctx := context.Background()
	simulatedErr := errors.New("disk full")

	// Capture log output to assert the warning was emitted
	var logBuf bytes.Buffer
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Mock provider and manager — never actually called because Run() is stubbed
	provider := &mockUnifiedHistoryProvider{
		GetHistoryStreamFunc: func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
			return []ports.HistoryViewDTO{}, "", nil
		},
	}
	hManager := &mockHistoryManagerFull{
		GetFilePathFunc: func() string { return "" },
	}

	browser := &tuiHistoryBrowser{
		logger: testLogger,
		initLogger: func() (io.Closer, error) {
			return &failingCloser{err: simulatedErr}, nil
		},
		newProgram: func(model tea.Model, opts ...tea.ProgramOption) programRunner {
			return &stubProgramRunner{}
		},
	}

	err := browser.Browse(ctx, provider, hManager)

	assert.NoError(t, err, "Browse should not fail when only logger close fails")
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "failed to close tui logger")
	assert.Contains(t, logOutput, simulatedErr.Error())
}
func TestTUIHistoryBrowser_Browse_ProgramRunError(t *testing.T) {
	ctx := context.Background()
	simulatedErr := errors.New("terminal disconnected")

	// Capture log to verify NO warning was emitted (logger close not reached)
	var logBuf bytes.Buffer
	testLogger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Mock provider and manager — never actually called because Run() is stubbed
	provider := &mockUnifiedHistoryProvider{
		GetHistoryStreamFunc: func(ctx context.Context, limit int, cursor string) ([]ports.HistoryViewDTO, string, error) {
			return []ports.HistoryViewDTO{}, "", nil
		},
	}
	hManager := &mockHistoryManagerFull{
		GetFilePathFunc: func() string { return "" },
	}

	browser := &tuiHistoryBrowser{
		logger: testLogger,
		// initLogger returns an error so the closer/defer is skipped entirely
		initLogger: func() (io.Closer, error) {
			return nil, errors.New("logger init skipped")
		},
		newProgram: func(model tea.Model, opts ...tea.ProgramOption) programRunner {
			return &failingProgramRunner{err: simulatedErr}
		},
	}

	err := browser.Browse(ctx, provider, hManager)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tui program error")
	assert.ErrorIs(t, err, simulatedErr,
		"Browse should wrap the program error with %w so callers can use errors.Is")

	// The warning log should NOT contain any close error since we skipped the logger
	logOutput := logBuf.String()
	assert.NotContains(t, logOutput, "failed to close tui logger")
}

func TestTeaProgramRunner_Run(t *testing.T) {
	p := tea.NewProgram(quitModel{}, tea.WithoutRenderer(), tea.WithInput(nil))
	runner := &teaProgramRunner{p: p}

	model, err := runner.Run()

	require.NoError(t, err, "teaProgramRunner.Run should not error with a simple model")
	require.NotNil(t, model)
	_, ok := model.(quitModel)
	assert.True(t, ok, "Run should return the model passed to NewProgram")
}

func TestDefaultUIFactory_HistoryBrowser_Constructor(t *testing.T) {
	sm := new(mockConfigurableSecurityManager)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	factory := newUIFactory(sm, io.Discard, io.Discard, logger)

	browser := factory.HistoryBrowser()
	require.NotNil(t, browser)

	tuiBrowser, ok := browser.(*tuiHistoryBrowser)
	require.True(t, ok, "HistoryBrowser should return *tuiHistoryBrowser")

	assert.Equal(t, logger, tuiBrowser.logger)
	assert.NotNil(t, tuiBrowser.initLogger, "initLogger should be the real tui.InitLogger")
	assert.NotNil(t, tuiBrowser.newProgram, "newProgram should be the real closure")

	runner := tuiBrowser.newProgram(quitModel{})
	require.NotNil(t, runner)
	_, ok = runner.(*teaProgramRunner)
	assert.True(t, ok, "newProgram should return a *teaProgramRunner")
}
