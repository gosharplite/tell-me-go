// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// mockSessionDeps is a field-based SessionDependencies used exclusively for
// nil-dependency injection tests. The testify mock in agenttest
// (MockServiceSessionDependencies) cannot inject nil because its
// m.Called().Get(0).(T) type assertions panic on nil interface values.
// Use this struct only when you need to test nil-dependency behavior;
// use MockServiceSessionDependencies for all other test scenarios.
type mockSessionDeps struct {
	gw              llm.LLMGateway
	hManager        ports.HistoryManager
	reg             tools.Registry
	sm              security.Manager
	bus             events.EventBus
	paths           *persistence.Paths
	tracker         pricing.CostTracker
	sessionProvider ports.SessionProvider
}

func (d *mockSessionDeps) GetGateway() llm.LLMGateway                           { return d.gw }
func (d *mockSessionDeps) GetHistoryManager() ports.HistoryManager              { return d.hManager }
func (d *mockSessionDeps) GetRegistry() (tools.Registry, error)                 { return d.reg, nil }
func (d *mockSessionDeps) GetSecurityManager() security.Manager                 { return d.sm }
func (d *mockSessionDeps) GetEventBus() events.EventBus                         { return d.bus }
func (d *mockSessionDeps) GetPaths() *persistence.Paths                         { return d.paths }
func (d *mockSessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing { return nil }
func (d *mockSessionDeps) GetTracker() pricing.CostTracker                      { return d.tracker }
func (d *mockSessionDeps) GetPricingData() pricing.PricingData                  { return pricing.PricingData{} }
func (d *mockSessionDeps) GetLogger() ports.Logger                              { return &ports.NoOpLogger{} }
func (d *mockSessionDeps) GetTurnsLogger() ports.TurnsLogger                    { return nil }
func (d *mockSessionDeps) GetSessionProvider() ports.SessionProvider            { return d.sessionProvider }
func (d *mockSessionDeps) GetHealthManager() ports.HealthCheckManager           { return nil }
func (d *mockSessionDeps) GetClient() llm.LLMClient                             { return nil }

type mockGateway struct {
	llm.LLMGateway
}

type mockHistoryManager struct {
	ports.HistoryManager
}

func (m *mockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

func (m *mockHistoryManager) GetFilePath() string { return "" }

type mockRegistry struct {
	tools.Registry
}

func (m *mockRegistry) Register(tool *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (m *mockRegistry) RegisterWithOptions(tool *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockRegistry) IsSerial(name string) bool {
	return false
}

func (m *mockRegistry) IsLongRunning(name string) bool {
	return false
}

func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return nil
}

func (m *mockRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *mockRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (m *mockRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (m *mockRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return nil
}

func (m *mockRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}

func (m *mockRegistry) ListAvailableToolkits() []string {
	return nil
}

type mockSecurityManager struct {
	security.Manager
}

func (m *mockSecurityManager) RegisterReadOnlyPath(path string) {}

func (m *mockSecurityManager) Close() error { return nil }

type mockTracker struct {
	pricing.CostTracker
}

func TestNewChatter(t *testing.T) {
	tmpDir := t.TempDir()
	modeDir := filepath.Join(tmpDir, "modes", "dev")
	err := os.MkdirAll(modeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create mode dir: %v", err)
	}

	// Create skills dir to satisfy NewFileSkillRepository
	skillsDir := filepath.Join(tmpDir, "docs", "skills")
	err = os.MkdirAll(skillsDir, 0755)
	if err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	deps := &mockSessionDeps{
		gw:       &mockGateway{},
		hManager: &mockHistoryManager{},
		reg:      &mockRegistry{},
		sm:       &mockSecurityManager{},
		bus:      bus,
		paths: &persistence.Paths{
			ModeDir: modeDir,
		},
		tracker: &mockTracker{},
	}

	cfg := ports.ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		LogPath:      filepath.Join(tmpDir, "trace.log"),
		TracePath:    filepath.Join(tmpDir, "trace.jsonl"),
	}

	t.Run("successful initialization", func(t *testing.T) {
		chatter, err := NewChatter(context.Background(), deps, cfg)
		if err != nil {
			t.Fatalf("NewChatter failed: %v", err)
		}

		if chatter == nil {
			t.Fatal("expected chatter to be non-nil")
		}
	})

	t.Run("fails if tool executor cannot be created", func(t *testing.T) {
		// agent_executor.NewToolExecutor fails if registry is nil
		depsFail := *deps
		depsFail.reg = nil

		_, err := NewChatter(context.Background(), &depsFail, cfg)
		if err == nil {
			t.Error("expected error due to nil registry, got nil")
		}
	})
}

// setupNilDepTest creates the temp directories and valid dependencies
// shared by all nil-dependency test cases. Callers mutate the returned
// deps to nil out the specific field under test.
func setupNilDepTest(t *testing.T) (*mockSessionDeps, ports.ChatterConfig) {
	t.Helper()
	tmpDir := t.TempDir()

	skillsDir := filepath.Join(tmpDir, "docs", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}
	modeDir := filepath.Join(tmpDir, "modes", "dev")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatalf("failed to create mode dir: %v", err)
	}

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })

	deps := &mockSessionDeps{
		gw:              &mockGateway{},
		hManager:        &mockHistoryManager{},
		reg:             &mockRegistry{},
		sm:              &mockSecurityManager{},
		bus:             bus,
		paths:           &persistence.Paths{ModeDir: modeDir},
		tracker:         &mockTracker{},
		sessionProvider: &agenttest.MockSessionProvider{},
	}

	cfg := ports.ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		Mode:         "chat",
		LogPath:      filepath.Join(tmpDir, "trace.log"),
		TracePath:    filepath.Join(tmpDir, "trace.jsonl"),
	}

	return deps, cfg
}

// callNewChatter wraps NewChatter with panic recovery so that unexpected panics
// are reported as test failures instead of crashing the test binary.
func callNewChatter(t *testing.T, deps *mockSessionDeps, cfg ports.ChatterConfig) (chatter ports.Chatter, err error) {
	t.Helper()
	var didPanic bool
	var panicVal any
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				panicVal = r
			}
		}()
		chatter, err = NewChatter(context.Background(), deps, cfg)
	}()
	if didPanic {
		t.Fatalf("BUG: NewChatter panicked unexpectedly: %v", panicVal)
	}
	return chatter, err
}

// assertNilDepRequired verifies that NewChatter returns an error containing
// wantErr when the dependency nil'd by setNil is missing.
func assertNilDepRequired(t *testing.T, deps *mockSessionDeps, cfg ports.ChatterConfig, setNil func(*mockSessionDeps), wantErr string) {
	t.Helper()
	setNil(deps)
	_, err := callNewChatter(t, deps, cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("expected error containing %q, got: %v", wantErr, err)
	}
}

// assertNilDepOptional verifies that NewChatter does not panic when the
// dependency nil'd by setNil is missing. Errors are logged but not fatal,
// since the production code tolerates nil for this dependency.
func assertNilDepOptional(t *testing.T, deps *mockSessionDeps, cfg ports.ChatterConfig, setNil func(*mockSessionDeps), label string) {
	t.Helper()
	setNil(deps)
	_, err := callNewChatter(t, deps, cfg)
	if err != nil {
		t.Logf("NOTE: nil %s returned error (unexpected but safe): %v", label, err)
	}
}

// TestNewChatter_NilDependencyError verifies that NewChatter returns a clear error
// (not a panic) when critical dependencies are nil. It uses the field-based
// mockSessionDeps struct so that nil can be injected directly without testify
// type-assertion interference.
func TestNewChatter_NilDependencyError(t *testing.T) {
	t.Run("nil gateway", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepRequired(t, deps, cfg, func(d *mockSessionDeps) { d.gw = nil }, "gateway is required")
	})

	t.Run("nil security manager", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepOptional(t, deps, cfg, func(d *mockSessionDeps) { d.sm = nil }, "security manager")
	})

	t.Run("nil registry", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepRequired(t, deps, cfg, func(d *mockSessionDeps) { d.reg = nil }, "failed to create tool executor")
	})

	t.Run("nil event bus", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepRequired(t, deps, cfg, func(d *mockSessionDeps) { d.bus = nil }, "event bus is required")
	})

	t.Run("nil history manager", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepRequired(t, deps, cfg, func(d *mockSessionDeps) { d.hManager = nil }, "history manager is required")
	})

	t.Run("nil paths", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepRequired(t, deps, cfg, func(d *mockSessionDeps) { d.paths = nil }, "paths is required")
	})

	t.Run("nil tracker", func(t *testing.T) {
		deps, cfg := setupNilDepTest(t)
		assertNilDepOptional(t, deps, cfg, func(d *mockSessionDeps) { d.tracker = nil }, "tracker")
	})
}
