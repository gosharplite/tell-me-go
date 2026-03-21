// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockLLMClient struct {
	mock.Mock
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, history, toolDecls, resolver)
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

func (m *mockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	args := m.Called(ctx, history, toolDecls, resolver, callback)
	return args.Get(0).(*llm.Metrics), args.Error(1)
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	args := m.Called(ctx, model, prompt, mimeType)
	return args.Get(0).([][]byte), args.Error(1)
}

func (m *mockLLMClient) RefreshAuth() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockLLMClient) Generate(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	return nil, nil
}

type mockConfigurableSecurityManager struct {
	mock.Mock
}

func (m *mockConfigurableSecurityManager) RegisterSafePath(path string)     { m.Called(path) }
func (m *mockConfigurableSecurityManager) RegisterReadOnlyPath(path string) { m.Called(path) }
func (m *mockConfigurableSecurityManager) SetCommandsLogFile(path string)   { m.Called(path) }
func (m *mockConfigurableSecurityManager) SetSafePathsFile(path string)     { m.Called(path) }
func (m *mockConfigurableSecurityManager) SetReadOnlyPathsFile(path string) { m.Called(path) }
func (m *mockConfigurableSecurityManager) SetBypassFile(path string)        { m.Called(path) }
func (m *mockConfigurableSecurityManager) LoadBypassState()                 { m.Called() }
func (m *mockConfigurableSecurityManager) LoadSafePaths() error {
	args := m.Called()
	return args.Error(0)
}
func (m *mockConfigurableSecurityManager) LoadReadOnlyPaths() error {
	args := m.Called()
	return args.Error(0)
}
func (m *mockConfigurableSecurityManager) RegisterPolicyTools(r tools.Registry) error {
	args := m.Called(r)
	return args.Error(0)
}

func (m *mockConfigurableSecurityManager) IsPathSafe(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockConfigurableSecurityManager) IsPathWritable(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockConfigurableSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (m *mockConfigurableSecurityManager) LogAudit(action string, args ...any) {}
func (m *mockConfigurableSecurityManager) Close() error                        { return nil }
func (m *mockConfigurableSecurityManager) TerminalLock()                       {}
func (m *mockConfigurableSecurityManager) TerminalUnlock()                     {}
func (m *mockConfigurableSecurityManager) Prompt(message string)               {}
func (m *mockConfigurableSecurityManager) Warn(message string)                 {}
func (m *mockConfigurableSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *mockConfigurableSecurityManager) ReadLine(ctx context.Context) (string, error) {
	return "", nil
}
func (m *mockConfigurableSecurityManager) IsCommandAllowed(command string) bool {
	return true
}
func (m *mockConfigurableSecurityManager) IsBypassActive() bool {
	return false
}

func setupDefaultSMExpectations(m *mockConfigurableSecurityManager) {
	m.On("LoadBypassState").Return().Maybe()
	m.On("SetSafePathsFile", mock.Anything).Return().Maybe()
	m.On("SetReadOnlyPathsFile", mock.Anything).Return().Maybe()
	m.On("SetBypassFile", mock.Anything).Return().Maybe()
	m.On("SetCommandsLogFile", mock.Anything).Return().Maybe()
	m.On("RegisterSafePath", mock.Anything).Return().Maybe()
	m.On("RegisterReadOnlyPath", mock.Anything).Return().Maybe()
}

func TestBuildSessionDependencies(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("LoadSafePaths").Return(nil).Maybe()
	sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil).Maybe()

	client := new(mockLLMClient)

	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})

	cfg := &config.Config{
		Mode: "assistant",
		Models: map[string]config.ModelConfig{
			"test-model": {
				Pricing: pricing.ModelPricing{Comp: 0.01},
			},
		},
		Model: "test-model",
	}

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	cleanup()
}

func TestGetAgentFactory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "di-test-factory")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)
}

func TestBootstrapper_Initialize_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-errors")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	simulatedErr := errors.New("simulated error")

	tests := []struct {
		name          string
		setup         func(t *testing.T) string // Returns homeDir for this test case
		clientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)
		mockSetup     func(sm *mockConfigurableSecurityManager)
		wantErr       string
		targetErr     error
	}{
		{
			name: "FailsOnInitializePathsError",
			setup: func(t *testing.T) string {
				invalidHome := filepath.Join(tempDir, "a-file")
				err := os.WriteFile(invalidHome, []byte("test"), 0644)
				require.NoError(t, err)
				return invalidHome
			},
			wantErr: "failed to create session directory",
		},
		{
			name: "FailsOnHistoryLoadError",
			setup: func(t *testing.T) string {
				home := filepath.Join(tempDir, "history-err-home")
				modeDir := filepath.Join(home, "output", cfg.Mode)
				err := os.MkdirAll(modeDir, 0755)
				require.NoError(t, err)
				historyPath := filepath.Join(modeDir, "history.jsonl")
				err = os.MkdirAll(historyPath, 0755) // Directory instead of file
				require.NoError(t, err)
				return home
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("LoadSafePaths").Return(nil).Maybe()
				sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
			},
			wantErr: "error loading history",
		},
		{
			name: "FailsOnBadClientFactory",
			setup: func(t *testing.T) string {
				return filepath.Join(tempDir, "factory-err-home")
			},
			clientFactory: func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
				return nil, simulatedErr
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("LoadSafePaths").Return(nil).Maybe()
				sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
			},
			targetErr: simulatedErr,
		},
		{
			name: "FailsOnLoadSafePathsError",
			setup: func(t *testing.T) string {
				return filepath.Join(tempDir, "safepaths-err-home")
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("LoadSafePaths").Return(simulatedErr)
			},
			wantErr:   "failed to load safe paths",
			targetErr: simulatedErr,
		},
		{
			name: "FailsOnLoadReadOnlyPathsError",
			setup: func(t *testing.T) string {
				return filepath.Join(tempDir, "readonly-err-home")
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("LoadSafePaths").Return(nil)
				sm.On("LoadReadOnlyPaths").Return(simulatedErr)
			},
			wantErr:   "failed to load read-only paths",
			targetErr: simulatedErr,
		},
		{
			name: "FailsOnRegisterPolicyToolsError",
			setup: func(t *testing.T) string {
				return filepath.Join(tempDir, "policy-err-home")
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("LoadSafePaths").Return(nil).Maybe()
				sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
				sm.On("RegisterPolicyTools", mock.Anything).Return(simulatedErr)
			},
			wantErr:   "error registering policy tools",
			targetErr: simulatedErr,
		},
		{
			name: "FailsOnStateInitError",
			setup: func(t *testing.T) string {
				home := filepath.Join(tempDir, "sessionstate-err-home")
				modeDir := filepath.Join(home, "output", cfg.Mode)
				err := os.MkdirAll(modeDir, 0755)
				require.NoError(t, err)
				// Create a directory where the db file should be to cause SQLite init to fail
				dbPath := filepath.Join(modeDir, "tellmego.db")
				err = os.MkdirAll(dbPath, 0755)
				require.NoError(t, err)
				return home
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("LoadSafePaths").Return(nil).Maybe()
				sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
			},
			wantErr: "failed to initialize session state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := tt.setup(t)
			sm := new(mockConfigurableSecurityManager)
			setupDefaultSMExpectations(sm)

			if tt.mockSetup != nil {
				tt.mockSetup(sm)
			}
			cf := tt.clientFactory
			if cf == nil {
				cf = func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
					return new(mockLLMClient), nil
				}
			}
			b := NewBootstrapper(homeDir, sm, "1.0.0", io.Discard, io.Discard, cf)
			newSession := strings.Contains(tt.name, "TriggerNewSession")
			_, _, _, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", newSession, nil)
			assert.Error(t, err)
			if err != nil {
				if tt.wantErr != "" {
					assert.Contains(t, err.Error(), tt.wantErr)
				}
				if tt.targetErr != nil {
					assert.ErrorIs(t, err, tt.targetErr)
				}
			}
		})
	}
}

func TestSucceedsWithWarningOnTriggerNewSession_RecordCostError(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-warning")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}
	simulatedErr := errors.New("simulated error")

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("LoadSafePaths").Return(nil).Maybe()
	sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil).Maybe()
	// RecordSessionCost -> EstimateCost -> IsPathSafe
	sm.On("IsPathSafe", mock.Anything).Return("", simulatedErr)

	var stderr bytes.Buffer
	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, &stderr, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	assert.Contains(t, stderr.String(), "Warning: Failed to record session cost for backup")
	assert.Contains(t, stderr.String(), simulatedErr.Error())

	cleanup()
}

type mockHistoryManager struct {
	ports.HistoryManager
	saveErr error
}

func (m *mockHistoryManager) Save(ctx context.Context) error {
	return m.saveErr
}

func TestFinalizeSession(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-finalize")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	client := new(mockLLMClient)
	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})

	sm.On("LoadSafePaths").Return(nil).Maybe()
	sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil).Maybe()
	sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()

	deps, hManager, cleanup, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, cleanup)
	defer cleanup()

	// Test success
	err = b.FinalizeSession(ctx, hManager, deps, cfg)
	assert.NoError(t, err)

	// Test with save error
	err = b.FinalizeSession(ctx, &mockHistoryManager{saveErr: errors.New("save failed")}, deps, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "save failed")

	// Test with record cost error
	sm.ExpectedCalls = nil
	setupDefaultSMExpectations(sm)
	sm.On("IsPathSafe", mock.Anything).Return("", errors.New("record cost failed"))

	err = b.FinalizeSession(ctx, hManager, deps, cfg)
	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "record cost failed")
	}

	// Test with both errors
	sm.ExpectedCalls = nil
	setupDefaultSMExpectations(sm)
	sm.On("IsPathSafe", mock.Anything).Return("", errors.New("record cost failed"))

	err = b.FinalizeSession(ctx, &mockHistoryManager{saveErr: errors.New("save failed")}, deps, cfg)
	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "save failed")
		assert.Contains(t, err.Error(), "record cost failed")
	}
}

func TestGetAgentFactory_Execution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "di-test-factory-exec")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)

	// Execute the factory
	bus := events.NewSimpleEventBus(context.Background())
	client := new(mockLLMClient)
	hManager := history.NewManager(nil, "history.jsonl", "archive.jsonl")
	reg := registry.New()

	mockDeps := &mockSessionDeps{
		gw:       client,
		hManager: hManager,
		reg:      reg,
		sm:       sm,
		bus:      bus,
		client:   client,
	}

	cfg := ports.ChatterConfig{
		ProviderName:     "test-provider",
		Model:            "test-model",
		Mode:             "assistant",
		LogPath:          "tokens.log",
		DisableStreaming: false,
	}
	agent, err := factory(context.Background(), mockDeps, cfg)
	assert.NoError(t, err)
	assert.NotNil(t, agent)
}

type mockSessionDeps struct {
	ports.SessionDependencies
	gw       llm.LLMGateway
	hManager ports.HistoryManager
	reg      tools.Registry
	sm       security.Manager
	bus      events.EventBus
	tracker  pricing.CostTracker
	paths    *persistence.Paths
	client   llm.LLMClient
}

func (m *mockSessionDeps) GetPaths() *persistence.Paths {
	if m.paths == nil {
		return &persistence.Paths{}
	}
	return m.paths
}
func (m *mockSessionDeps) GetPricingData() pricing.PricingData     { return pricing.PricingData{} }
func (m *mockSessionDeps) GetGateway() llm.LLMGateway              { return m.gw }
func (m *mockSessionDeps) GetHistoryManager() ports.HistoryManager { return m.hManager }
func (m *mockSessionDeps) GetRegistry() tools.Registry             { return m.reg }
func (m *mockSessionDeps) GetSecurityManager() security.Manager {
	return m.sm
}
func (m *mockSessionDeps) GetEventBus() events.EventBus { return m.bus }
func (m *mockSessionDeps) GetTracker() pricing.CostTracker {
	if m.tracker == nil {
		return &mockTracker{}
	}
	return m.tracker
}
func (m *mockSessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing { return nil }
func (m *mockSessionDeps) GetClient() llm.LLMClient                             { return m.client }

type mockTracker struct {
	pricing.CostTracker
}

func (m *mockTracker) Warmup() {}

func TestBuildSessionDependencies_NewSession(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-new-session")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("LoadSafePaths").Return(nil).Maybe()
	sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil).Maybe()
	sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()

	client := new(mockLLMClient)

	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})

	cfg := &config.Config{
		Mode: "assistant",
		Models: map[string]config.ModelConfig{
			"test-model": {
				Pricing: pricing.ModelPricing{Comp: 0.01},
			},
		},
		Model: "test-model",
	}

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	cleanup()
}

func TestSessionDeps_Getters(t *testing.T) {
	paths := &persistence.Paths{}
	hManager := &history.Manager{}
	client := &mockLLMClient{}
	gw := client
	reg := registry.New()
	sm := new(mockConfigurableSecurityManager)
	bus := events.NewSimpleEventBus(context.Background())
	tracker := &mockTracker{}
	pData := pricing.PricingData{}

	deps := &sessionDeps{
		paths:       paths,
		hManager:    hManager,
		client:      client,
		gw:          gw,
		reg:         reg,
		sm:          sm,
		bus:         bus,
		tracker:     tracker,
		pricingData: pData,
	}

	assert.Equal(t, gw, deps.GetGateway())
	assert.Equal(t, hManager, deps.GetHistoryManager())
	assert.Equal(t, reg, deps.GetRegistry())
	assert.Equal(t, sm, deps.GetSecurityManager())
	assert.Equal(t, bus, deps.GetEventBus())
	assert.Equal(t, paths, deps.GetPaths())
	assert.Equal(t, tracker, deps.GetTracker())
	assert.Equal(t, pData, deps.GetPricingData())
	assert.Equal(t, client, deps.GetClient())
}

type mockSessionProvider struct {
	mock.Mock
}

func (m *mockSessionProvider) GetTasks() ports.TaskStore            { return nil }
func (m *mockSessionProvider) GetScratchpad() ports.ScratchpadStore { return nil }
func (m *mockSessionProvider) GetInfo() ports.SessionInfo {
	args := m.Called()
	return args.Get(0).(ports.SessionInfo)
}
func (m *mockSessionProvider) SetInfo(info ports.SessionInfo) {
	m.Called(info)
}
func (m *mockSessionProvider) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestContainer_InitializationErrors(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-init-errors")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	simulatedErr := errors.New("simulated error")
	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	tests := []struct {
		name      string
		mockSetup func(b *bootstrapper, sm *mockConfigurableSecurityManager)
		wantErr   string
	}{
		{
			name: "ToolRegistrationFails",
			mockSetup: func(b *bootstrapper, sm *mockConfigurableSecurityManager) {
				b.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error {
					return simulatedErr
				}
			},
			wantErr: "error registering tools: simulated error",
		},
		{
			name: "TelemetryRegistrationFails",
			mockSetup: func(b *bootstrapper, sm *mockConfigurableSecurityManager) {
				b.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing) error {
					return simulatedErr
				}
			},
			wantErr: "error registering metrics tools: simulated error",
		},
		{
			name: "SessionRotationFails",
			mockSetup: func(b *bootstrapper, sm *mockConfigurableSecurityManager) {
				b.RotateSession = func(fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error {
					return simulatedErr
				}
				sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()
			},
			wantErr: "session initialization failed during rotation: session rotation failed: simulated error",
		},
		{
			name: "SessionProviderCloseFails",
			mockSetup: func(b *bootstrapper, sm *mockConfigurableSecurityManager) {
				mockSP := new(mockSessionProvider)
				mockSP.On("GetInfo").Return(ports.SessionInfo{}).Maybe()
				mockSP.On("SetInfo", mock.Anything).Return().Maybe()
				mockSP.On("Close").Return(simulatedErr)

				b.NewSessionState = func(ctx context.Context, modeDir string) (ports.SessionProvider, error) {
					return mockSP, nil
				}
			},
			wantErr: "", // No error from BuildSessionDependencies
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)
			setupDefaultSMExpectations(sm)
			sm.On("LoadSafePaths").Return(nil).Maybe()
			sm.On("LoadReadOnlyPaths").Return(nil).Maybe()
			sm.On("RegisterPolicyTools", mock.Anything).Return(nil).Maybe()

			var stderr bytes.Buffer
			b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, &stderr, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
				return new(mockLLMClient), nil
			}).(*bootstrapper)

			if tt.mockSetup != nil {
				tt.mockSetup(b, sm)
			}

			newSession := (tt.name == "SessionRotationFails")
			_, _, cleanup, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", newSession, nil)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
				if cleanup != nil {
					cleanup()
					if tt.name == "SessionProviderCloseFails" {
						assert.Contains(t, stderr.String(), "Warning: Failed to close session provider")
					}
				}
			}
		})
	}
}
