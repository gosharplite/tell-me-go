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

// TestNewChatter_NilDependencyError verifies that NewChatter returns a clear error
// (not a panic) when critical dependencies are nil. It uses the field-based
// mockSessionDeps struct so that nil can be injected directly without testify
// type-assertion interference.
func TestNewChatter_NilDependencyError(t *testing.T) {
	sharedConfig := func(tmpDir string) ports.ChatterConfig {
		return ports.ChatterConfig{
			ProviderName: "test-provider",
			Model:        "test-model",
			Mode:         "chat",
			LogPath:      filepath.Join(tmpDir, "trace.log"),
			TracePath:    filepath.Join(tmpDir, "trace.jsonl"),
		}
	}

	validDeps := func(t *testing.T, tmpDir string) *mockSessionDeps {
		t.Helper()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })
		return &mockSessionDeps{
			gw:              &mockGateway{},
			hManager:        &mockHistoryManager{},
			reg:             &mockRegistry{},
			sm:              &mockSecurityManager{},
			bus:             bus,
			paths:           &persistence.Paths{ModeDir: filepath.Join(tmpDir, "modes", "dev")},
			tracker:         &mockTracker{},
			sessionProvider: &agenttest.MockSessionProvider{},
		}
	}

	tests := []struct {
		name      string
		nilFields []string
		wantErr   string // empty = expect no error
		byDesign  bool   // true = nil is intentionally tolerated, not a bug
	}{
		{
			name:      "nil gateway",
			nilFields: []string{"gw"},
			wantErr:   "gateway is required",
		},
		{
			name:      "nil security manager",
			nilFields: []string{"sm"},
			wantErr:   "",
		},
		{
			name:      "nil registry",
			nilFields: []string{"reg"},
			wantErr:   "failed to create tool executor",
		},
		{
			name:      "nil event bus",
			nilFields: []string{"bus"},
			wantErr:   "event bus is required",
		},
		{
			name:      "nil history manager",
			nilFields: []string{"hManager"},
			wantErr:   "history manager is required",
		},
		{
			name:      "nil paths",
			nilFields: []string{"paths"},
			wantErr:   "paths is required",
		},
		{
			name:      "nil tracker",
			nilFields: []string{"tracker"},
			wantErr:   "",
			byDesign:  true, // Engine tolerates nil CostTracker per Reconfigure comment
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			skillsDir := filepath.Join(tmpDir, "docs", "skills")
			if err := os.MkdirAll(skillsDir, 0755); err != nil {
				t.Fatalf("failed to create skills dir: %v", err)
			}
			modeDir := filepath.Join(tmpDir, "modes", "dev")
			if err := os.MkdirAll(modeDir, 0755); err != nil {
				t.Fatalf("failed to create mode dir: %v", err)
			}

			deps := validDeps(t, tmpDir)
			for _, f := range tt.nilFields {
				switch f {
				case "gw":
					deps.gw = nil
				case "hManager":
					deps.hManager = nil
				case "reg":
					deps.reg = nil
				case "sm":
					deps.sm = nil
				case "bus":
					deps.bus = nil
				case "paths":
					deps.paths = nil
				case "tracker":
					deps.tracker = nil
				}
			}
			cfg := sharedConfig(tmpDir)

			var didPanic bool
			var panicVal any
			var chatter ports.Chatter
			var err error
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
				if tt.wantErr != "" {
					t.Errorf("expected error containing %q, but got panic: %v", tt.wantErr, panicVal)
				} else {
					t.Errorf("BUG: NewChatter panicked on %s instead of returning an error: %v", tt.name, panicVal)
				}
				return
			}

			if tt.wantErr == "" {
				if err != nil {
					t.Logf("NOTE: %s returned error (unexpected but safe): %v", tt.name, err)
				} else if tt.byDesign {
					t.Logf("NOTE: %s produced no error (by design — engine nil-tolerant)", tt.name)
				} else {
					t.Errorf("BUG: %s produced no error (silent nil acceptance); chatter is non-nil=%v", tt.name, chatter != nil)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
				}
			}
		})
	}
}
