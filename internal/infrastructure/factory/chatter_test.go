// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	"context"
	"errors"
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
	"github.com/stretchr/testify/require"
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
	regErr          error // if non-nil, GetRegistry returns this error
	sm              security.Manager
	bus             events.EventBus
	paths           *persistence.Paths
	tracker         pricing.CostTracker
	sessionProvider ports.SessionProvider
}

func (d *mockSessionDeps) GetGateway() llm.LLMGateway              { return d.gw }
func (d *mockSessionDeps) GetHistoryManager() ports.HistoryManager { return d.hManager }
func (d *mockSessionDeps) GetRegistry() (tools.Registry, error) {
	if d.regErr != nil {
		return nil, d.regErr
	}
	return d.reg, nil
}
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

	t.Run("fails when skill repo cannot load", func(t *testing.T) {
		deps2, cfg2 := setupNilDepTest(t)
		// setupNilDepTest creates a docs/skills/ dir. Drop an unreadable
		// .md file into it to cause NewFileSkillRepository to fail.
		skillsDir := filepath.Join(filepath.Dir(filepath.Dir(deps2.paths.ModeDir)), "docs", "skills")
		badFile := filepath.Join(skillsDir, "bad.md")
		require.NoError(t, os.WriteFile(badFile,
			[]byte("---\nname: Test\ndescription: Test\n---\nContent"), 0644))
		require.NoError(t, os.Chmod(badFile, 0000))
		t.Cleanup(func() { _ = os.Chmod(badFile, 0644) })

		_, err := NewChatter(context.Background(), deps2, cfg2)
		if err == nil || !strings.Contains(err.Error(), "failed to initialize skill repository") {
			t.Errorf("expected 'failed to initialize skill repository' error, got: %v", err)
		}
	})

	t.Run("fails when registry cannot be retrieved", func(t *testing.T) {
		deps2, cfg2 := setupNilDepTest(t)
		deps2.regErr = errors.New("registry unavailable")

		_, err := NewChatter(context.Background(), deps2, cfg2)
		if err == nil || !strings.Contains(err.Error(), "failed to retrieve tool registry") {
			t.Errorf("expected 'failed to retrieve tool registry' error, got: %v", err)
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

// TestResolveSkillsDir exercises all three branches of resolveSkillsDir:
// 1. Home-dir skills directory exists (happy path, already covered indirectly)
// 2. Home-dir missing → CWD fallback succeeds
// 3. Both missing → returns home path anyway
//
// The os.Getwd error branch (line 30) is intentionally left uncovered as
// it guards against a theoretical OS failure that cannot be triggered on
// a real operating system.
func TestResolveSkillsDir(t *testing.T) {
	t.Run("home dir skills exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		modeDir := filepath.Join(tmpDir, "modes", "dev")
		require.NoError(t, os.MkdirAll(modeDir, 0755))
		skillsDir := filepath.Join(tmpDir, "docs", "skills")
		require.NoError(t, os.MkdirAll(skillsDir, 0755))

		paths := &persistence.Paths{ModeDir: modeDir}
		got := resolveSkillsDir(paths)
		expected := filepath.Clean(skillsDir)
		if got != expected {
			t.Errorf("resolveSkillsDir() = %q, want %q", got, expected)
		}
	})

	t.Run("cwd fallback when home dir missing", func(t *testing.T) {
		// home tmp dir — no docs/skills/
		homeTmp := t.TempDir()
		modeDir := filepath.Join(homeTmp, "modes", "dev")
		require.NoError(t, os.MkdirAll(modeDir, 0755))

		// cwd tmp dir — HAS docs/skills/
		cwdTmp := t.TempDir()
		cwdSkills := filepath.Join(cwdTmp, "docs", "skills")
		require.NoError(t, os.MkdirAll(cwdSkills, 0755))

		// Switch CWD to cwdTmp. t.Chdir handles cleanup automatically.
		t.Chdir(cwdTmp)

		paths := &persistence.Paths{ModeDir: modeDir}
		got := resolveSkillsDir(paths)
		expected := filepath.Clean(cwdSkills)
		if got != expected {
			t.Errorf("resolveSkillsDir() = %q, want %q (cwd fallback)", got, expected)
		}
	})

	t.Run("both missing returns home path anyway", func(t *testing.T) {
		homeTmp := t.TempDir()
		modeDir := filepath.Join(homeTmp, "modes", "dev")
		require.NoError(t, os.MkdirAll(modeDir, 0755))

		cwdTmp := t.TempDir()
		t.Chdir(cwdTmp) // cwdTmp has no docs/skills/

		paths := &persistence.Paths{ModeDir: modeDir}
		got := resolveSkillsDir(paths)
		expected := filepath.Clean(filepath.Join(homeTmp, "docs", "skills"))
		if got != expected {
			t.Errorf("resolveSkillsDir() = %q, want %q (home path fallback)", got, expected)
		}
	})
}
