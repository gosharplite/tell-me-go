// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
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

func (m *mockConfigurableSecurityManager) SetCommandsLogFile(path string)  {}
func (m *mockConfigurableSecurityManager) SetSafePathsFile(path string)     {}
func (m *mockConfigurableSecurityManager) SetReadOnlyPathsFile(path string) {}
func (m *mockConfigurableSecurityManager) SetBypassFile(path string)         {}
func (m *mockConfigurableSecurityManager) LoadSafePaths() error {
	args := m.Called()
	return args.Error(0)
}
func (m *mockConfigurableSecurityManager) LoadReadOnlyPaths() error {
	args := m.Called()
	return args.Error(0)
}
func (m *mockConfigurableSecurityManager) LoadBypassState()                 {}
func (m *mockConfigurableSecurityManager) RegisterSafePath(path string)     {}
func (m *mockConfigurableSecurityManager) RegisterReadOnlyPath(path string) {}
func (m *mockConfigurableSecurityManager) RegisterPolicyTools(r tools.IToolRegistry) error {
	args := m.Called(r)
	return args.Error(0)
}

func (m *mockConfigurableSecurityManager) IsPathSafe(path string) (string, error) {
	return path, nil
}
func (m *mockConfigurableSecurityManager) IsPathWritable(path string) (string, error) {
	return path, nil
}
func (m *mockConfigurableSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (m *mockConfigurableSecurityManager) LogAudit(action string, args ...any) {}
func (m *mockConfigurableSecurityManager) Close() error                         { return nil }
func (m *mockConfigurableSecurityManager) TerminalLock()                        {}
func (m *mockConfigurableSecurityManager) TerminalUnlock()                      {}
func (m *mockConfigurableSecurityManager) Prompt(message string)                {}
func (m *mockConfigurableSecurityManager) Warn(message string)                  {}
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

func TestBuildSessionDependencies(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	sm.On("LoadSafePaths").Return(nil)
	sm.On("LoadReadOnlyPaths").Return(nil)
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil)
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
				sm.On("LoadSafePaths").Return(nil)
				sm.On("LoadReadOnlyPaths").Return(nil)
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
				sm.On("LoadSafePaths").Return(nil)
				sm.On("LoadReadOnlyPaths").Return(nil)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := tt.setup(t)
			sm := new(mockConfigurableSecurityManager)
			if tt.mockSetup != nil {
				tt.mockSetup(sm)
			}
			b := NewBootstrapper(homeDir, sm, "1.0.0", io.Discard, io.Discard, tt.clientFactory)
			_, _, _, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
			assert.Error(t, err)
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
			}
			if tt.targetErr != nil {
				assert.ErrorIs(t, err, tt.targetErr)
			}
		})
	}
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
	sm.On("LoadSafePaths").Return(nil)
	sm.On("LoadReadOnlyPaths").Return(nil)
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil)

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	client := new(mockLLMClient)
	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})

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
}

func TestGetAgentFactory_Execution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "di-test-factory-exec")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)

	// Execute the factory
	bus := events.NewSimpleEventBus()
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
	reg      tools.IToolRegistry
	sm       security.ISecurityManager
	bus      events.EventBus
	tracker  pricing.ICostTracker
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
func (m *mockSessionDeps) GetRegistry() tools.IToolRegistry        { return m.reg }
func (m *mockSessionDeps) GetSecurityManager() security.ISecurityManager {
	return m.sm
}
func (m *mockSessionDeps) GetEventBus() events.EventBus { return m.bus }
func (m *mockSessionDeps) GetTracker() pricing.ICostTracker {
	if m.tracker == nil {
		return &mockTracker{}
	}
	return m.tracker
}
func (m *mockSessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing { return nil }
func (m *mockSessionDeps) GetClient() llm.LLMClient                             { return m.client }

type mockTracker struct {
	pricing.ICostTracker
}

func (m *mockTracker) Warmup() {}

func TestBuildSessionDependencies_NewSession(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-new-session")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	sm.On("LoadSafePaths").Return(nil)
	sm.On("LoadReadOnlyPaths").Return(nil)
	sm.On("RegisterPolicyTools", mock.Anything).Return(nil)
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
	bus := events.NewSimpleEventBus()
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
