// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
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
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

type mockLLMClient struct {
	mock.Mock
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, history, toolDecls, resolver)
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	args := m.Called(ctx, model, prompt, mimeType)
	return args.Get(0).([][]byte), args.Error(1)
}

func (m *mockLLMClient) RefreshAuth() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockLLMClient) Generate(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, input, toolDecls, resolver)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

type mockConfigurableSecurityManager struct {
	mock.Mock
}

func (m *mockConfigurableSecurityManager) RegisterSafePath(path string)     { m.Called(path) }
func (m *mockConfigurableSecurityManager) RegisterReadOnlyPath(path string) { m.Called(path) }
func (m *mockConfigurableSecurityManager) SetCommandsLogFile(path string)   { m.Called(path) }
func (m *mockConfigurableSecurityManager) SetBypassActive(active bool)      { m.Called(active) }
func (m *mockConfigurableSecurityManager) RegisterPolicyTools(r tools.Registry, kv ports.KVStore) error {
	args := m.Called(r, kv)
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
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()

	client := new(mockLLMClient)

	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
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

	deps, hManager, cleanup, err := Bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	_ = cleanup(ctx)
}

func TestGetAgentFactory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "di-test-factory")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, nil)

	factory := Bootstrapper.GetAgentFactory()
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
			},
			wantErr: "failed to load history from",
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
				sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
				sm.On("SetBypassActive", mock.Anything).Return().Maybe()
			},
			wantErr: "", // No longer fails here
		},
		{
			name: "FailsOnRegisterPolicyToolsError",
			setup: func(t *testing.T) string {
				return filepath.Join(tempDir, "policy-err-home")
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(simulatedErr)
				sm.On("SetBypassActive", mock.Anything).Return().Maybe()
			},
			wantErr: "", // No longer fails here because registration is lazy
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
			b := NewBootstrapper(homeDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, cf)
			newSession := strings.Contains(tt.name, "TriggerNewSession")
			_, _, _, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", newSession, nil)

			if tt.wantErr == "" && tt.targetErr == nil {
				assert.NoError(t, err)
				return
			}

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
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()
	// RecordSessionCost -> EstimateCost -> IsPathSafe
	sm.On("IsPathSafe", mock.Anything).Return("", simulatedErr)

	var stderr bytes.Buffer
	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, &stderr, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})

	deps, hManager, cleanup, err := Bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	assert.Contains(t, stderr.String(), "Warning: Failed to record session cost for backup")
	assert.Contains(t, stderr.String(), simulatedErr.Error())

	_ = cleanup(ctx)
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
	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})

	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()
	sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()

	deps, hManager, cleanup, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, cleanup)
	defer func() { _ = cleanup(ctx) }()

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
	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, nil)

	factory := Bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)

	// Execute the factory
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
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
		ProviderName: "test-provider",
		Model:        "test-model",
		Mode:         "assistant",
		LogPath:      "tokens.log",
		TracePath:    "tokens.trace.jsonl",
	}
	agent, err := factory(context.Background(), mockDeps, cfg)
	assert.NoError(t, err)
	assert.NotNil(t, agent)
}

type mockSessionDeps struct {
	ports.SessionDependencies
	gw              llm.LLMGateway
	hManager        ports.HistoryManager
	reg             tools.Registry
	sm              security.Manager
	bus             events.EventBus
	tracker         pricing.CostTracker
	paths           *persistence.Paths
	client          llm.LLMClient
	sessionProvider ports.SessionProvider
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
func (m *mockSessionDeps) GetRegistry() (tools.Registry, error)    { return m.reg, nil }
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
func (m *mockSessionDeps) GetLogger() *slog.Logger                              { return slog.Default() }
func (m *mockSessionDeps) GetTurnsLogger() ports.TurnsLogger {
	return &ports.NoOpTurnsLogger{}
}
func (m *mockSessionDeps) GetSessionProvider() ports.SessionProvider { return m.sessionProvider }

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
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()
	sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()

	client := new(mockLLMClient)

	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
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

	deps, hManager, cleanup, err := Bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	_ = cleanup(ctx)
}

func TestSessionDeps_Getters(t *testing.T) {
	paths := &persistence.Paths{}
	hManager := &history.Manager{}
	client := &mockLLMClient{}
	gw := client
	reg := registry.New()
	sm := new(mockConfigurableSecurityManager)
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)
	tracker := &mockTracker{}
	pData := pricing.PricingData{}
	sessionProvider := new(mockSessionProvider)

	deps := &sessionDeps{
		paths:           paths,
		hManager:        hManager,
		client:          client,
		gw:              gw,
		reg:             reg,
		sm:              sm,
		bus:             bus,
		tracker:         tracker,
		pricingData:     pData,
		sessionProvider: sessionProvider,
		clientFactory: func() (llm.ExtendedClient, error) {
			return client, nil
		},
		regFactory: func() (tools.Registry, error) {
			return reg, nil
		},
	}

	assert.NotNil(t, deps.GetGateway())
	assert.Equal(t, hManager, deps.GetHistoryManager())
	regGot, regErr := deps.GetRegistry()
	assert.Equal(t, reg, regGot)
	assert.NoError(t, regErr)
	assert.Equal(t, sm, deps.GetSecurityManager())
	assert.Equal(t, bus, deps.GetEventBus())
	assert.Equal(t, paths, deps.GetPaths())
	assert.Equal(t, tracker, deps.GetTracker())
	assert.Equal(t, pData, deps.GetPricingData())
	assert.NotNil(t, deps.GetClient())
	assert.Equal(t, sessionProvider, deps.GetSessionProvider())
}

type mockKVStore struct {
	mock.Mock
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}
func (m *mockKVStore) Set(ctx context.Context, key, val string) error {
	args := m.Called(ctx, key, val)
	return args.Error(0)
}
func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}
func (m *mockKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]string), args.Error(1)
}

type mockSessionProvider struct {
	mock.Mock
}

func (m *mockSessionProvider) GetTasks() ports.TaskStore { return nil }
func (m *mockSessionProvider) GetSettings() ports.KVStore {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.KVStore)
}
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
		mockSetup func(b *Bootstrapper, sm *mockConfigurableSecurityManager)
		wantErr   string
	}{
		{
			name: "ToolRegistrationFails",
			mockSetup: func(b *Bootstrapper, sm *mockConfigurableSecurityManager) {
				b.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error {
					return simulatedErr
				}
			},
			wantErr: "", // Lazy initialization
		},
		{
			name: "TelemetryRegistrationFails",
			mockSetup: func(b *Bootstrapper, sm *mockConfigurableSecurityManager) {
				b.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
					return simulatedErr
				}
			},
			wantErr: "", // Lazy initialization
		},
		{
			name: "SessionRotationFails",
			mockSetup: func(b *Bootstrapper, sm *mockConfigurableSecurityManager) {
				b.RotateSession = func(ctx context.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int) error {
					return simulatedErr
				}
				sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()
			},
			wantErr: "session initialization failed during rotation for",
		},
		{
			name: "SessionProviderCloseFails",
			mockSetup: func(b *Bootstrapper, sm *mockConfigurableSecurityManager) {
				mockSP := new(mockSessionProvider)
				mockKV := new(mockKVStore)
				mockKV.On("Get", mock.Anything, mock.Anything).Return("", nil).Maybe()
				mockSP.On("GetSettings").Return(mockKV).Maybe()
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
			sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
			sm.On("SetBypassActive", mock.Anything).Return().Maybe()

			var stderr bytes.Buffer
			b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, &stderr, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
				return new(mockLLMClient), nil
			})

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
					_ = cleanup(ctx)
					if tt.name == "SessionProviderCloseFails" {
						assert.Contains(t, stderr.String(), "Warning: Failed to close session provider")
					}
				}
			}
		})
	}
}

func TestCrossSessionPersistence(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	mode := "assistant"

	// 1. Manually create the DB and seed the bypass setting
	dbDir := filepath.Join(tempDir, "output", mode)
	err := os.MkdirAll(dbDir, 0755)
	require.NoError(t, err)
	dbPath := filepath.Join(dbDir, "tellmego.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO settings VALUES ('bypass_confirmation', 'true')")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// 2. Setup SM Mock
	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("SetBypassActive", true).Return() // Expect this!
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()

	// 3. Build Dependencies
	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})
	cfg := &config.Config{Mode: mode, Model: "test-model"}
	_, _, cleanup, err := Bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)

	// 4. Verification
	assert.NoError(t, err)
	sm.AssertCalled(t, "SetBypassActive", true)
	if cleanup != nil {
		_ = cleanup(ctx)
	}
}

func TestApplySessionSecuritySettings_LogErrors(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	sm := new(mockConfigurableSecurityManager)
	mockKV := new(mockKVStore)
	mockSP := new(mockSessionProvider)
	mockSP.On("GetSettings").Return(mockKV)

	// Inject invalid JSON for paths
	mockKV.On("Get", mock.Anything, "bypass_confirmation").Return("false", nil)
	mockKV.On("Get", mock.Anything, "authorized_safe_paths").Return("invalid-json", nil)
	mockKV.On("Get", mock.Anything, "authorized_read_paths").Return("invalid-json", nil)

	// Capture logs
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	factory := &defaultSessionFactory{
		SM:     sm,
		Logger: logger,
	}

	factory.applySessionSecuritySettings(ctx, mockSP)

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "failed to unmarshal authorized_safe_paths")
	assert.Contains(t, logOutput, "failed to unmarshal authorized_read_paths")
	assert.Contains(t, logOutput, "invalid-json")
}

func TestGetSuggestionService(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)

	Bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, nil)

	svc, err := Bootstrapper.GetSuggestionService(ctx, []string{"test prompt"})
	assert.NoError(t, err)
	assert.NotNil(t, svc)

	suggestions, err := svc.GetSuggestions(ctx, "test")
	assert.NoError(t, err)
	assert.NotEmpty(t, suggestions)
	assert.Contains(t, suggestions, "test prompt")
}

func TestBootstrapper_Cleanup_ClosesTurnsLogger(t *testing.T) {
	ctx := context.Background()
	// 1. Setup minimal dependencies for the bootstrapper
	tmpDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()

	bootstrapper := NewBootstrapper(tmpDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})

	cfg := &config.Config{
		Mode: "assistant",
	}

	// 2. Execute BuildSessionDependencies
	deps, _, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "dummy-path.yaml", true, nil)

	require.NoError(t, err)
	require.NotNil(t, cleanup)
	require.NotNil(t, deps.GetTurnsLogger())

	// 3. Execute the returned cleanup chain
	err = cleanup(ctx)
	assert.NoError(t, err, "Cleanup chain should execute without errors")

	// 4. Verify idempotency and closure
	// Calling Close() again on the AsyncTurnsLogger should return nil immediately if it was already closed.
	err = deps.GetTurnsLogger().Close()
	assert.NoError(t, err, "Subsequent Close() calls should be a no-op")
}

func TestGetChatService(t *testing.T) {
	tempDir := t.TempDir()
	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)

	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, nil)
	svc := b.GetChatService()
	assert.NotNil(t, svc)
}

type mockFileWithErrors struct {
	infra_persistence.File
	closeErr error
}

func (m *mockFileWithErrors) Close() error                { return m.closeErr }
func (m *mockFileWithErrors) Sync() error                 { return nil }
func (m *mockFileWithErrors) Write(p []byte) (int, error) { return len(p), nil }

type mockFSWithErrors struct {
	infra_persistence.FileSystem
	file infra_persistence.File
}

func (m *mockFSWithErrors) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	return m.file, nil
}
func (m *mockFSWithErrors) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}
func (m *mockFSWithErrors) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

func TestBootstrapper_Cleanup_ChainsErrors(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// 1. Setup mocks
	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()
	sm.On("IsPathSafe", mock.Anything).Return("safe", nil).Maybe()

	busErr := errors.New("bus shutdown failed")
	mockSP := new(mockSessionProvider)
	mockKV := new(mockKVStore)
	mockKV.On("Get", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockSP.On("GetSettings").Return(mockKV).Maybe()
	mockSP.On("GetInfo").Return(ports.SessionInfo{}).Maybe()
	mockSP.On("SetInfo", mock.Anything).Return().Maybe()
	mockSP.On("Close").Return(busErr)

	logErr := errors.New("log flush failed")
	file := &mockFileWithErrors{closeErr: logErr}
	fs := &mockFSWithErrors{file: file}

	bootstrapper := NewBootstrapper(tmpDir, sm, "1.0.0", io.Discard, io.Discard, nil, fs, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})

	bootstrapper.NewSessionState = func(ctx context.Context, modeDir string) (ports.SessionProvider, error) {
		return mockSP, nil
	}

	cfg := &config.Config{
		Mode: "assistant",
	}

	// 2. Build dependencies
	_, _, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	// 3. Execute cleanup and verify error chaining
	err = cleanup(ctx)
	require.Error(t, err)

	// Verify both errors are present in the joined error
	assert.ErrorIs(t, err, busErr)
	assert.ErrorIs(t, err, logErr)
	assert.Contains(t, err.Error(), "bus shutdown failed")
	assert.Contains(t, err.Error(), "log flush failed")
}
