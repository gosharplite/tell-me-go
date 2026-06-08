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
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// ---- Hand-rolled mocks (no testify/mock) ----

type mockLLMClient struct {
	SendChatFunc       func(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	GenerateImagesFunc func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	RefreshAuthFunc    func() error
	GenerateFunc       func(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)

	sendChatCalls       int
	generateImagesCalls int
	refreshAuthCalls    int
	generateCalls       int
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.sendChatCalls++
	if m.SendChatFunc != nil {
		return m.SendChatFunc(ctx, history, toolDecls, resolver)
	}
	return &llm.Content{}, &llm.Metrics{}, nil
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	m.generateImagesCalls++
	if m.GenerateImagesFunc != nil {
		return m.GenerateImagesFunc(ctx, model, prompt, mimeType)
	}
	return nil, nil
}

func (m *mockLLMClient) RefreshAuth() error {
	m.refreshAuthCalls++
	if m.RefreshAuthFunc != nil {
		return m.RefreshAuthFunc()
	}
	return nil
}

func (m *mockLLMClient) Generate(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.generateCalls++
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, toolDecls, resolver)
	}
	return &llm.Content{}, &llm.Metrics{}, nil
}

// mockClientFactory implements ports.ClientFactory for testing failover paths.
type mockClientFactory struct {
	NewClientFunc        func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)
	NewFailoverChainFunc func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error)

	newClientCalls        int
	newFailoverChainCalls int
}

func (m *mockClientFactory) NewClient(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
	m.newClientCalls++
	if m.NewClientFunc != nil {
		return m.NewClientFunc(cfg, pricingData, bus, logger)
	}
	return nil, nil
}

func (m *mockClientFactory) NewFailoverChain(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
	m.newFailoverChainCalls++
	if m.NewFailoverChainFunc != nil {
		return m.NewFailoverChainFunc(cfg, pricingData, bus, logger)
	}
	return nil, nil
}

type mockKVStore struct {
	GetFunc    func(ctx context.Context, key string) (string, error)
	SetFunc    func(ctx context.Context, key, val string) error
	DeleteFunc func(ctx context.Context, key string) error
	GetAllFunc func(ctx context.Context) (map[string]string, error)

	GetCalls    int
	SetCalls    int
	DeleteCalls int
	GetAllCalls int
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	m.GetCalls++
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key)
	}
	return "", nil
}

func (m *mockKVStore) Set(ctx context.Context, key, val string) error {
	m.SetCalls++
	if m.SetFunc != nil {
		return m.SetFunc(ctx, key, val)
	}
	return nil
}

func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	m.DeleteCalls++
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, key)
	}
	return nil
}

func (m *mockKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	m.GetAllCalls++
	if m.GetAllFunc != nil {
		return m.GetAllFunc(ctx)
	}
	return map[string]string{}, nil
}

type mockSessionProvider struct {
	GetTasksFunc         func() ports.TaskStore
	GetSettingsFunc      func() ports.KVStore
	GetInfoFunc          func() ports.SessionInfo
	SetInfoFunc          func(info ports.SessionInfo)
	CloseFunc            func() error
	GetHealthCheckerFunc func() ports.HealthChecker

	getTasksCalls         int
	getSettingsCalls      int
	getInfoCalls          int
	setInfoCalls          int
	closeCalls            int
	getHealthCheckerCalls int
}

func (m *mockSessionProvider) GetTasks() ports.TaskStore {
	m.getTasksCalls++
	if m.GetTasksFunc != nil {
		return m.GetTasksFunc()
	}
	return nil
}

func (m *mockSessionProvider) GetSettings() ports.KVStore {
	m.getSettingsCalls++
	if m.GetSettingsFunc != nil {
		return m.GetSettingsFunc()
	}
	return nil
}

func (m *mockSessionProvider) GetInfo() ports.SessionInfo {
	m.getInfoCalls++
	if m.GetInfoFunc != nil {
		return m.GetInfoFunc()
	}
	return ports.SessionInfo{}
}

func (m *mockSessionProvider) SetInfo(info ports.SessionInfo) {
	m.setInfoCalls++
	if m.SetInfoFunc != nil {
		m.SetInfoFunc(info)
	}
}

func (m *mockSessionProvider) Close() error {
	m.closeCalls++
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *mockSessionProvider) GetHealthChecker() ports.HealthChecker {
	m.getHealthCheckerCalls++
	if m.GetHealthCheckerFunc != nil {
		return m.GetHealthCheckerFunc()
	}
	return nil
}

type mockConfigurableSecurityManager struct {
	RegisterSafePathFunc     func(path string)
	RegisterReadOnlyPathFunc func(path string)
	SetCommandsLogFileFunc   func(path string)
	SetBypassActiveFunc      func(active bool)
	RegisterPolicyToolsFunc  func(r tools.Registry, kv ports.KVStore) error
	IsPathSafeFunc           func(path string) (string, error)
	IsPathWritableFunc       func(path string) (string, error)

	registerSafePathCalls     int
	registerReadOnlyPathCalls int
	setCommandsLogFileCalls   int
	setBypassActiveCalls      int
	registerPolicyToolsCalls  int
	isPathSafeCalls           int
	isPathWritableCalls       int
	authorizeCalls            int
	logAuditCalls             int
	closeCalls                int
	terminalLockCalls         int
	terminalUnlockCalls       int
	promptCalls               int
	warnCalls                 int
	confirmCalls              int
	readLineCalls             int
	isCommandAllowedCalls     int
	isBypassActiveCalls       int
}

func (m *mockConfigurableSecurityManager) Reset() { *m = mockConfigurableSecurityManager{} }

func (m *mockConfigurableSecurityManager) RegisterSafePath(path string) {
	m.registerSafePathCalls++
	if m.RegisterSafePathFunc != nil {
		m.RegisterSafePathFunc(path)
	}
}

func (m *mockConfigurableSecurityManager) RegisterReadOnlyPath(path string) {
	m.registerReadOnlyPathCalls++
	if m.RegisterReadOnlyPathFunc != nil {
		m.RegisterReadOnlyPathFunc(path)
	}
}

func (m *mockConfigurableSecurityManager) SetCommandsLogFile(path string) {
	m.setCommandsLogFileCalls++
	if m.SetCommandsLogFileFunc != nil {
		m.SetCommandsLogFileFunc(path)
	}
}

func (m *mockConfigurableSecurityManager) SetBypassActive(active bool) {
	m.setBypassActiveCalls++
	if m.SetBypassActiveFunc != nil {
		m.SetBypassActiveFunc(active)
	}
}

func (m *mockConfigurableSecurityManager) RegisterPolicyTools(r tools.Registry, kv ports.KVStore) error {
	m.registerPolicyToolsCalls++
	if m.RegisterPolicyToolsFunc != nil {
		return m.RegisterPolicyToolsFunc(r, kv)
	}
	return nil
}

func (m *mockConfigurableSecurityManager) IsPathSafe(path string) (string, error) {
	m.isPathSafeCalls++
	if m.IsPathSafeFunc != nil {
		return m.IsPathSafeFunc(path)
	}
	return "safe", nil
}

func (m *mockConfigurableSecurityManager) IsPathWritable(path string) (string, error) {
	m.isPathWritableCalls++
	if m.IsPathWritableFunc != nil {
		return m.IsPathWritableFunc(path)
	}
	return "writable", nil
}

func (m *mockConfigurableSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	m.authorizeCalls++
	return true, nil
}

func (m *mockConfigurableSecurityManager) LogAudit(action string, args ...any) { m.logAuditCalls++ }

func (m *mockConfigurableSecurityManager) Close() error { m.closeCalls++; return nil }

func (m *mockConfigurableSecurityManager) TerminalLock()         { m.terminalLockCalls++ }
func (m *mockConfigurableSecurityManager) TerminalUnlock()       { m.terminalUnlockCalls++ }
func (m *mockConfigurableSecurityManager) Prompt(message string) { m.promptCalls++ }
func (m *mockConfigurableSecurityManager) Warn(message string)   { m.warnCalls++ }

func (m *mockConfigurableSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	m.confirmCalls++
	return true, nil
}

func (m *mockConfigurableSecurityManager) ReadLine(ctx context.Context) (string, error) {
	m.readLineCalls++
	return "", nil
}

func (m *mockConfigurableSecurityManager) IsCommandAllowed(command string) bool {
	m.isCommandAllowedCalls++
	return true
}

func (m *mockConfigurableSecurityManager) IsBypassActive() bool {
	m.isBypassActiveCalls++
	return false
}

// setupDefaultSMExpectations is now a no-op — hand-rolled mocks have safe zero values.
func setupDefaultSMExpectations(m *mockConfigurableSecurityManager) {}

// ---- Tests ----

func TestBuildSessionDependencies(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	sm := new(mockConfigurableSecurityManager)

	client := new(mockLLMClient)

	cfg := DefaultBootstrapperConfig()
	cfg.HomeDir = tempDir
	cfg.SM = sm
	cfg.Version = "1.0.0"
	cfg.Stdout = io.Discard
	cfg.Stderr = io.Discard
	cfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})
	bootstrapper := NewBootstrapper(cfg)

	testCfg := &config.Config{
		Mode: "assistant",
		Models: map[string]config.ModelConfig{
			"test-model": {
				Pricing: pricing.ModelPricing{Comp: 0.01},
			},
		},
		Model: "test-model",
	}

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
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

	cfg := DefaultBootstrapperConfig()
	cfg.HomeDir = tempDir
	cfg.SM = sm
	cfg.Version = "1.0.0"
	cfg.Stdout = io.Discard
	cfg.Stderr = io.Discard
	bootstrapper := NewBootstrapper(cfg)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)
}

func TestBootstrapper_Initialize_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-errors")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	testCfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	simulatedErr := errors.New("simulated error")

	tests := []struct {
		name          string
		setup         func(t *testing.T) string // Returns homeDir for this test case
		clientFactory ports.ClientFactory
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
				modeDir := filepath.Join(home, "output", testCfg.Mode)
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
			clientFactory: ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
				return nil, simulatedErr
			}),
			mockSetup: func(sm *mockConfigurableSecurityManager) {
			},
			wantErr: "", // No longer fails here
		},
		{
			name: "FailsOnRegisterPolicyToolsError",
			setup: func(t *testing.T) string {
				return filepath.Join(tempDir, "policy-err-home")
			},
			mockSetup: func(sm *mockConfigurableSecurityManager) {
				sm.RegisterPolicyToolsFunc = func(r tools.Registry, kv ports.KVStore) error {
					return simulatedErr
				}
			},
			wantErr: "", // No longer fails here because registration is lazy
		},
		{
			name: "FailsOnStateInitError",
			setup: func(t *testing.T) string {
				home := filepath.Join(tempDir, "sessionstate-err-home")
				modeDir := filepath.Join(home, "output", testCfg.Mode)
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

			if tt.mockSetup != nil {
				tt.mockSetup(sm)
			}
			cf := tt.clientFactory
			if cf == nil {
				cf = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
					return new(mockLLMClient), nil
				})
			}
			bcfg := DefaultBootstrapperConfig()
			bcfg.HomeDir = homeDir
			bcfg.SM = sm
			bcfg.Version = "1.0.0"
			bcfg.Stdout = io.Discard
			bcfg.Stderr = io.Discard
			bcfg.ClientFactory = cf
			b := NewBootstrapper(bcfg)
			newSession := strings.Contains(tt.name, "TriggerNewSession")
			_, _, _, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", newSession, nil)

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

	testCfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}
	simulatedErr := errors.New("simulated error")

	sm := new(mockConfigurableSecurityManager)
	// RecordSessionCost -> EstimateCost -> IsPathSafe
	sm.IsPathSafeFunc = func(path string) (string, error) {
		return "", simulatedErr
	}

	var stderr bytes.Buffer
	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = &stderr
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})
	bootstrapper := NewBootstrapper(bcfg)

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, testCfg, "config.yaml", true, nil)
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

	testCfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	client := new(mockLLMClient)
	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})
	b := NewBootstrapper(bcfg)

	deps, hManager, cleanup, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, cleanup)
	defer func() { _ = cleanup(ctx) }()

	// Test success
	err = b.FinalizeSession(ctx, hManager, deps, testCfg)
	assert.NoError(t, err)

	// Test with save error
	err = b.FinalizeSession(ctx, &mockHistoryManager{saveErr: errors.New("save failed")}, deps, testCfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "save failed")

	// Test with record cost error
	sm.Reset()
	sm.IsPathSafeFunc = func(path string) (string, error) {
		return "", errors.New("record cost failed")
	}

	err = b.FinalizeSession(ctx, hManager, deps, testCfg)
	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "record cost failed")
	}

	// Test with both errors
	sm.Reset()
	sm.IsPathSafeFunc = func(path string) (string, error) {
		return "", errors.New("record cost failed")
	}

	err = b.FinalizeSession(ctx, &mockHistoryManager{saveErr: errors.New("save failed")}, deps, testCfg)
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

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bootstrapper := NewBootstrapper(bcfg)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)

	// Execute the factory
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
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

	testCfg := ports.ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		Mode:         "assistant",
		LogPath:      "tokens.log",
		TracePath:    "tokens.trace.jsonl",
	}
	agent, err := factory(context.Background(), mockDeps, testCfg)
	assert.NoError(t, err)
	assert.NotNil(t, agent)
}

type mockSessionDeps struct {
	ports.ChatterComposer
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
func (m *mockSessionDeps) GetLogger() ports.Logger                              { return &ports.NoOpLogger{} }
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

	client := new(mockLLMClient)

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})
	bootstrapper := NewBootstrapper(bcfg)

	testCfg := &config.Config{
		Mode: "assistant",
		Models: map[string]config.ModelConfig{
			"test-model": {
				Pricing: pricing.ModelPricing{Comp: 0.01},
			},
		},
		Model: "test-model",
	}

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, testCfg, "config.yaml", true, nil)
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
	reg := registry.New()
	sm := new(mockConfigurableSecurityManager)
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	tracker := &mockTracker{}
	sessionProvider := new(mockSessionProvider)

	lazyClient := newLazyClient(func() (llm.ExtendedClient, error) {
		return client, nil
	})
	lazyRegistry := newLazyRegistry(func() (tools.Registry, error) {
		return reg, nil
	}, &ports.NoOpLogger{})

	deps := &sessionDeps{
		infraProvider: infraProvider{
			paths: paths,
			sm:    sm,
			bus:   bus,
		},
		telemetryProvider: telemetryProvider{
			tracker: tracker,
		},
		sessionStateProvider: sessionStateProvider{
			hManager:        hManager,
			sessionProvider: sessionProvider,
			workspacePolicy: infra_persistence.NewWorkspacePolicy(),
		},
		lazyProvider: lazyProvider{
			client:   lazyClient,
			registry: lazyRegistry,
		},
		healthProvider: healthProvider{
			health: factory.NewHealthCheckManager(nil),
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
	assert.NotNil(t, deps.GetClient())
	assert.Equal(t, sessionProvider, deps.GetSessionProvider())
	assert.NotNil(t, deps.GetHealthManager())
	assert.NotNil(t, deps.GetWorkspacePolicy())
}

func TestContainer_InitializationErrors(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-init-errors")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	simulatedErr := errors.New("simulated error")
	testCfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	tests := []struct {
		name      string
		cfgSetup  func(cfg *BootstrapperConfig, sm *mockConfigurableSecurityManager)
		mockSetup func(sm *mockConfigurableSecurityManager)
		wantErr   string
	}{
		{
			name: "ToolRegistrationFails",
			cfgSetup: func(cfg *BootstrapperConfig, sm *mockConfigurableSecurityManager) {
				cfg.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error {
					return simulatedErr
				}
			},
			wantErr: "",
		},
		{
			name: "TelemetryRegistrationFails",
			cfgSetup: func(cfg *BootstrapperConfig, sm *mockConfigurableSecurityManager) {
				cfg.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
					return simulatedErr
				}
			},
			wantErr: "",
		},
		{
			name: "SessionRotationFails",
			cfgSetup: func(cfg *BootstrapperConfig, sm *mockConfigurableSecurityManager) {
				cfg.RotateSession = func(ctx context.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int, logger *slog.Logger) error {
					return simulatedErr
				}
			},
			wantErr: "session initialization failed during rotation for",
		},
		{
			name: "SessionProviderCloseFails",
			cfgSetup: func(cfg *BootstrapperConfig, sm *mockConfigurableSecurityManager) {
				mockSP := &mockSessionProvider{
					GetSettingsFunc: func() ports.KVStore {
						return &mockKVStore{}
					},
					CloseFunc: func() error {
						return simulatedErr
					},
				}

				cfg.NewSessionState = func(ctx context.Context, modeDir string) (ports.SessionProvider, error) {
					return mockSP, nil
				}
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)

			var stderr bytes.Buffer
			bcfg := DefaultBootstrapperConfig()
			bcfg.HomeDir = tempDir
			bcfg.SM = sm
			bcfg.Version = "1.0.0"
			bcfg.Stdout = io.Discard
			bcfg.Stderr = &stderr
			bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
				return new(mockLLMClient), nil
			})

			if tt.cfgSetup != nil {
				tt.cfgSetup(&bcfg, sm)
			}
			if tt.mockSetup != nil {
				tt.mockSetup(sm)
			}

			b := NewBootstrapper(bcfg)

			newSession := (tt.name == "SessionRotationFails")
			_, _, cleanup, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", newSession, nil)

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

	// 2. Setup SM Mock — track SetBypassActive call
	sm := new(mockConfigurableSecurityManager)

	// 3. Build Dependencies
	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})
	bootstrapper := NewBootstrapper(bcfg)
	testCfg := &config.Config{Mode: mode, Model: "test-model"}
	_, _, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)

	// 4. Verification
	assert.NoError(t, err)
	assert.Equal(t, 1, sm.setBypassActiveCalls, "SetBypassActive should be called once")
	if cleanup != nil {
		_ = cleanup(ctx)
	}
}

func TestApplySessionSecuritySettings_LogErrors(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	sm := new(mockConfigurableSecurityManager)
	mockKV := &mockKVStore{
		GetFunc: func(ctx context.Context, key string) (string, error) {
			switch key {
			case "bypass_confirmation":
				return "false", nil
			case "authorized_safe_paths", "authorized_read_paths":
				return "invalid-json", nil
			}
			return "", nil
		},
	}
	mockSP := &mockSessionProvider{
		GetSettingsFunc: func() ports.KVStore { return mockKV },
	}

	// Capture logs
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	factory := &defaultSessionFactory{
		SM:     sm,
		Logger: logger,
	}

	factory.applySessionSecuritySettings(ctx, mockSP, &config.Config{})

	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "failed to unmarshal authorized_safe_paths")
	assert.Contains(t, logOutput, "failed to unmarshal authorized_read_paths")
	assert.Contains(t, logOutput, "invalid-json")
}

func TestLoadPathsFromSettings_EmptyValueAndSuccessPath(t *testing.T) {
	ctx := context.Background()

	t.Run("empty value returns early without registering", func(t *testing.T) {
		mockKV := &mockKVStore{
			GetFunc: func(ctx context.Context, key string) (string, error) {
				return "", nil
			},
		}

		sm := new(mockConfigurableSecurityManager)

		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))

		loadPathsFromSettings(ctx, mockKV, "authorized_safe_paths", sm.RegisterSafePath, logger)

		// Verify no log output (no error, no registration)
		assert.Empty(t, logBuf.String())
		assert.Equal(t, 0, sm.registerSafePathCalls, "RegisterSafePath should not be called")
	})

	t.Run("valid JSON registers all paths", func(t *testing.T) {
		mockKV := &mockKVStore{
			GetFunc: func(ctx context.Context, key string) (string, error) {
				return `["/tmp/a","/tmp/b"]`, nil
			},
		}

		var registeredPaths []string
		sm := &mockConfigurableSecurityManager{
			RegisterSafePathFunc: func(path string) {
				registeredPaths = append(registeredPaths, path)
			},
		}

		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))

		loadPathsFromSettings(ctx, mockKV, "authorized_safe_paths", sm.RegisterSafePath, logger)

		assert.Empty(t, logBuf.String())
		assert.Equal(t, []string{"/tmp/a", "/tmp/b"}, registeredPaths)
	})
}

func TestGetSuggestionService(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bootstrapper := NewBootstrapper(bcfg)

	svc, err := bootstrapper.GetSuggestionService(ctx, []string{"test prompt"})
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

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tmpDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})
	bootstrapper := NewBootstrapper(bcfg)

	testCfg := &config.Config{
		Mode: "assistant",
	}

	// 2. Execute BuildSessionDependencies
	deps, _, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, testCfg, "dummy-path.yaml", true, nil)

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

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)
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

	busErr := errors.New("bus shutdown failed")
	mockSP := &mockSessionProvider{
		GetSettingsFunc: func() ports.KVStore {
			return &mockKVStore{}
		},
		CloseFunc: func() error {
			return busErr
		},
	}

	logErr := errors.New("log flush failed")
	file := &mockFileWithErrors{closeErr: logErr}
	fs := &mockFSWithErrors{file: file}

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tmpDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.FileSystem = fs
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})
	bcfg.NewSessionState = func(ctx context.Context, modeDir string) (ports.SessionProvider, error) {
		return mockSP, nil
	}
	bootstrapper := NewBootstrapper(bcfg)

	testCfg := &config.Config{
		Mode: "assistant",
	}

	// 2. Build dependencies
	_, _, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
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

func TestBootstrapper_GetPricingOverrides(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := &Bootstrapper{cfg: BootstrapperConfig{Logger: logger}}
	testCfg := &config.Config{
		Models: map[string]config.ModelConfig{
			"model1": {
				Pricing: pricing.ModelPricing{Comp: 0.1},
			},
			"model2": {
				Pricing: pricing.ModelPricing{Comp: 0}, // Should be skipped
			},
			"model3": {
				Pricing: pricing.ModelPricing{Comp: 0.2},
			},
		},
	}

	overrides := b.getPricingOverrides(testCfg)
	assert.Len(t, overrides, 2)
	assert.Contains(t, overrides, "model1")
	assert.Contains(t, overrides, "model3")
	assert.NotContains(t, overrides, "model2")
	assert.Equal(t, 0.1, overrides["model1"].Comp)
	assert.Equal(t, 0.2, overrides["model3"].Comp)
}

func TestGetUnifiedHistoryProvider_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	sm := new(mockConfigurableSecurityManager)

	// Create a file where a directory should be to cause EnsureDirectories to fail
	conflictFile := filepath.Join(tmpDir, "output")
	if err := os.WriteFile(conflictFile, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tmpDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)
	testCfg := &config.Config{Mode: "assistant"}

	provider, err := b.GetUnifiedHistoryProvider(context.Background(), testCfg, nil)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

func TestGetSuggestionService_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	sm := new(mockConfigurableSecurityManager)

	// Conflict to make suggestion service init fail if it tried to do something,
	// but currently it only logs warning and uses no-op tracker if NewGlobalPromptTracker fails.
	// Let's trigger NewGlobalPromptTracker failure.
	conflictFile := filepath.Join(tmpDir, "output")
	if err := os.WriteFile(conflictFile, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tmpDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)

	svc, err := b.GetSuggestionService(context.Background(), nil)
	// Suggestion service itself doesn't fail, it just uses NoOpTracker
	assert.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestGetHistoryManager_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	sm := new(mockConfigurableSecurityManager)

	// Conflict to make EnsureDirectories fail
	conflictFile := filepath.Join(tmpDir, "output")
	if err := os.WriteFile(conflictFile, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tmpDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)
	testCfg := &config.Config{Mode: "assistant"}

	hManager, err := b.GetHistoryManager(context.Background(), testCfg)
	assert.Error(t, err)
	assert.Nil(t, hManager)
}

func TestSessionDeps_GetRegistry_Failure(t *testing.T) {
	deps := &sessionDeps{
		lazyProvider: lazyProvider{
			registry: newLazyRegistry(func() (tools.Registry, error) {
				return nil, errors.New("registry failed")
			}, telemetry.NewSlogLogger(nil)),
		},
	}
	reg, err := deps.GetRegistry()
	assert.Error(t, err)
	assert.Nil(t, reg)
}

func TestBypassConfirmationPriority(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		yamlBypass   bool
		dbBypass     string
		expectActive bool
	}{
		{
			name:         "YAML_True_DB_False_Active",
			yamlBypass:   true,
			dbBypass:     "false",
			expectActive: true,
		},
		{
			name:         "YAML_False_DB_True_Active",
			yamlBypass:   false,
			dbBypass:     "true",
			expectActive: true,
		},
		{
			name:         "YAML_False_DB_False_Inactive",
			yamlBypass:   false,
			dbBypass:     "false",
			expectActive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBypassActive bool
			var bypassCallCount int
			sm := &mockConfigurableSecurityManager{
				SetBypassActiveFunc: func(active bool) {
					capturedBypassActive = active
					bypassCallCount++
				},
			}

			mockKV := &mockKVStore{
				GetFunc: func(ctx context.Context, key string) (string, error) {
					switch key {
					case "bypass_confirmation":
						return tt.dbBypass, nil
					default:
						return "", nil
					}
				},
			}
			mockSP := &mockSessionProvider{
				GetSettingsFunc: func() ports.KVStore { return mockKV },
			}

			factory := &defaultSessionFactory{
				SM:     sm,
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}

			testCfg := &config.Config{BypassConfirmation: tt.yamlBypass}
			factory.applySessionSecuritySettings(ctx, mockSP, testCfg)

			if tt.expectActive {
				assert.Equal(t, 1, bypassCallCount, "SetBypassActive should be called once")
				assert.True(t, capturedBypassActive, "SetBypassActive should be called with true")
			} else {
				assert.Equal(t, 0, bypassCallCount, "SetBypassActive should not be called")
			}
		})
	}
}

func TestHandleNewSession_RetentionDays(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("rotate failed")

	tests := []struct {
		name              string
		kvValue           string
		kvErr             error
		expectedRetention int
	}{
		{
			name:              "KV Get error falls back to default 30",
			kvValue:           "",
			kvErr:             errors.New("db error"),
			expectedRetention: 30,
		},
		{
			name:              "KV returns empty string falls back to default 30",
			kvValue:           "",
			kvErr:             nil,
			expectedRetention: 30,
		},
		{
			name:              "KV returns valid integer parses correctly",
			kvValue:           "7",
			kvErr:             nil,
			expectedRetention: 7,
		},
		{
			name:              "KV returns non-integer falls back to default 30",
			kvValue:           "not-a-number",
			kvErr:             nil,
			expectedRetention: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockKV := &mockKVStore{
				GetFunc: func(ctx context.Context, key string) (string, error) {
					return tt.kvValue, tt.kvErr
				},
			}

			sm := new(mockConfigurableSecurityManager)

			var capturedRetention int
			rotateCalled := make(chan int, 1)
			rotateFunc := func(ctx context.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int, logger *slog.Logger) error {
				rotateCalled <- retentionDays
				return simulatedErr
			}

			factory := &defaultSessionFactory{
				HomeDir:       tempDir,
				FileSystem:    &infra_persistence.OSFileSystem{},
				SM:            sm,
				Stdout:        io.Discard,
				Stderr:        io.Discard,
				Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
				RotateSession: rotateFunc,
			}

			paths := &persistence.Paths{
				LogPath: filepath.Join(tempDir, "test.log"),
			}
			testCfg := &config.Config{Mode: "assistant", Model: "test-model"}

			_ = factory.handleNewSession(ctx, paths, testCfg, nil, mockKV)

			select {
			case capturedRetention = <-rotateCalled:
			default:
				t.Fatal("RotateSession was not called")
			}

			assert.Equal(t, tt.expectedRetention, capturedRetention)
		})
	}
}

func TestBootstrapper_Getters(t *testing.T) {
	tempDir := t.TempDir()
	sm := new(mockConfigurableSecurityManager)

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)

	assert.NotNil(t, b.GetHistoryBrowser())
	assert.NotNil(t, b.GetUIRenderer())
	assert.NotNil(t, b.GetHistoryRenderer())
}

func TestGetUnifiedHistoryProvider_SuccessPath(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)

	client := new(mockLLMClient)
	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.ClientFactory = ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})
	b := NewBootstrapper(bcfg)
	testCfg := &config.Config{Mode: "assistant"}

	// Build a real HistoryManager first (exercises BuildHistoryManager success)
	hManager, err := b.GetHistoryManager(ctx, testCfg)
	require.NoError(t, err)
	require.NotNil(t, hManager)

	// Exercise BuildUnifiedHistoryProvider success path (gap 1)
	provider, err := b.GetUnifiedHistoryProvider(ctx, testCfg, hManager)
	assert.NoError(t, err)
	assert.NotNil(t, provider)

	// Exercise BuildRegistry success path (gap 2) through a full session build
	// This tests the real defaultToolchainFactory.BuildRegistry return reg, nil
	deps, _, cleanup, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
	require.NoError(t, err)
	defer func() { _ = cleanup(ctx) }()

	// Trigger lazy registry init → exercises real BuildRegistry
	reg, err := deps.GetRegistry()
	assert.NoError(t, err)
	assert.NotNil(t, reg)
}

func TestNewBootstrapper_DefaultFallbacks(t *testing.T) {
	cfg := BootstrapperConfig{
		HomeDir: t.TempDir(),
		Version: "test",
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		// ClientFactory, Logger, FileSystem, WorkspacePolicy — all deliberately nil
		// to exercise the default-assignment branches in NewBootstrapper.
	}
	b := NewBootstrapper(cfg)
	assert.NotNil(t, b.GetChatService())
	assert.NotNil(t, b.GetHistoryBrowser())
	assert.NotNil(t, b.GetUIRenderer())
	assert.NotNil(t, b.GetHistoryRenderer())
}

func TestBuildSessionDependencies_FailoverChain(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("failover init failed")

	testCfg := &config.Config{
		Mode:          "assistant",
		Model:         "test-model",
		FailoverOrder: []string{"provider-a", "provider-b"},
	}

	tests := []struct {
		name            string
		mockSetup       func(factory *mockClientFactory, gwClient *mockExtendedClient)
		expectNewClient bool
		expectGwUsed    bool
		wantGenerateErr error
	}{
		{
			name: "NewFailoverChain_ReturnsError",
			mockSetup: func(factory *mockClientFactory, gwClient *mockExtendedClient) {
				factory.NewFailoverChainFunc = func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
					return nil, simulatedErr
				}
			},
			expectNewClient: false,
			expectGwUsed:    false,
			wantGenerateErr: simulatedErr,
		},
		{
			name: "NewFailoverChain_ReturnsNilNil_FallsBackToNewClient",
			mockSetup: func(factory *mockClientFactory, gwClient *mockExtendedClient) {
				factory.NewFailoverChainFunc = func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
					return nil, nil
				}
				factory.NewClientFunc = func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
					return gwClient, nil
				}
				gwClient.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{}, &llm.Metrics{}, nil
				}
			},
			expectNewClient: true,
			expectGwUsed:    true,
			wantGenerateErr: nil,
		},
		{
			name: "NewFailoverChain_ReturnsGateway_UsesGateway",
			mockSetup: func(factory *mockClientFactory, gwClient *mockExtendedClient) {
				factory.NewFailoverChainFunc = func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
					return gwClient, nil
				}
				gwClient.GenerateFunc = func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{}, &llm.Metrics{}, nil
				}
			},
			expectNewClient: false,
			expectGwUsed:    true,
			wantGenerateErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)

			gwClient := new(mockExtendedClient)
			factory := new(mockClientFactory)
			if tt.mockSetup != nil {
				tt.mockSetup(factory, gwClient)
			}

			bcfg := DefaultBootstrapperConfig()
			bcfg.HomeDir = tempDir
			bcfg.SM = sm
			bcfg.Version = "1.0.0"
			bcfg.Stdout = io.Discard
			bcfg.Stderr = io.Discard
			bcfg.ClientFactory = factory
			b := NewBootstrapper(bcfg)

			deps, _, cleanup, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
			require.NoError(t, err)
			require.NotNil(t, deps)
			defer func() { _ = cleanup(ctx) }()

			// Trigger lazy client initialization
			gw := deps.GetGateway()
			_, _, genErr := gw.Generate(ctx, nil, nil, nil)

			if tt.wantGenerateErr != nil {
				require.Error(t, genErr)
				assert.ErrorIs(t, genErr, tt.wantGenerateErr,
					"Generate should propagate the failover chain error")
				assert.Contains(t, genErr.Error(), "LLM provider initialization failed")
			} else {
				require.NoError(t, genErr)
			}

			if tt.expectNewClient {
				assert.Equal(t, 1, factory.newClientCalls, "NewClient should be called")
			} else {
				assert.Equal(t, 0, factory.newClientCalls, "NewClient should not be called")
			}

			if tt.expectGwUsed {
				assert.Equal(t, 1, gwClient.generateCalls)
			}
		})
	}
}

func TestWireInfrastructure(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := &Bootstrapper{cfg: BootstrapperConfig{Logger: logger}}

	bus, log := b.wireInfrastructure(ctx)

	assert.NotNil(t, bus, "event bus should not be nil")
	assert.NotNil(t, log, "logger should not be nil")
}

func TestWireTelemetry(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)

	tf := newTelemetryFactory(tempDir, &infra_persistence.OSFileSystem{}, sm, nil)
	b := &Bootstrapper{telemetryFactory: tf}

	paths := &persistence.Paths{TurnsLogPath: filepath.Join(tempDir, "turns.log")}
	cfg := &config.Config{Model: "test-model"}

	origCleanup := func(ctx context.Context) error { return nil }
	pricingData, tracker, turnsLogger, cleanup := b.wireTelemetry(ctx, paths, cfg, nil, origCleanup)

	assert.NotNil(t, pricingData)
	assert.NotNil(t, tracker)
	assert.NotNil(t, turnsLogger)
	assert.NotNil(t, cleanup)
}

func TestWireLLMClient(t *testing.T) {
	ctx := context.Background()

	client := &mockLLMClient{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{}, &llm.Metrics{}, nil
		},
	}

	cf := ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		return client, nil
	})
	b := &Bootstrapper{cfg: BootstrapperConfig{ClientFactory: cf}}

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	logger := telemetry.NewSlogLogger(nil)
	cfg := &config.Config{Model: "test-model"}

	lc := b.wireLLMClient(cfg, pricing.PricingData{}, bus, logger)

	assert.NotNil(t, lc)
	// Verify lazy init works by calling Generate, which triggers factory init
	_, _, err := lc.Generate(ctx, nil, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, client.generateCalls, "Generate should be called once")
}

func TestWireHealth(t *testing.T) {
	tempDir := t.TempDir()
	sm := new(mockConfigurableSecurityManager)

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)

	mockSP := &mockSessionProvider{
		GetSettingsFunc: func() ports.KVStore {
			return &mockKVStore{}
		},
	}

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})

	cfg := &config.Config{Mode: "assistant"}
	hm := b.wireHealth(cfg, mockSP, lc)

	assert.NotNil(t, hm)
}

func TestWireToolRegistry(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)

	paths := &persistence.Paths{LogPath: filepath.Join(tempDir, "test.log"), TracePath: filepath.Join(tempDir, "test.trace.jsonl")}
	mockSP := &mockSessionProvider{
		GetSettingsFunc: func() ports.KVStore {
			return &mockKVStore{}
		},
	}

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return new(mockLLMClient), nil
	})

	hm := b.wireHealth(&config.Config{Mode: "assistant"}, mockSP, lc)

	cfg := &config.Config{Model: "test-model", Mode: "assistant"}
	lr := b.wireToolRegistry(paths, mockSP, hm, lc, bus, cfg, nil, nil)

	assert.NotNil(t, lr)
	// Verify lazy init works
	reg, err := lr.get()
	assert.NoError(t, err)
	assert.NotNil(t, reg)
}

func TestLogBuildStart(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	b := &Bootstrapper{cfg: BootstrapperConfig{Logger: logger}}

	cfg := &config.Config{
		Model: "test-model",
		Models: map[string]config.ModelConfig{
			"test-model": {Pricing: pricing.ModelPricing{Comp: 0.01}},
		},
	}

	b.logBuildStart(cfg, "/path/to/config.yaml")

	output := buf.String()
	assert.Contains(t, output, "Building session dependencies")
	assert.Contains(t, output, "test-model")
	assert.Contains(t, output, "/path/to/config.yaml")
}
