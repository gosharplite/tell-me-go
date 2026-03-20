// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockSessionDeps struct {
	gw       llm.LLMGateway
	hManager ports.HistoryManager
	reg      tools.Registry
	sm       security.ISecurityManager
	bus      events.EventBus
	paths    *persistence.Paths
	tracker  pricing.CostTracker
}

func (d *mockSessionDeps) GetGateway() llm.LLMGateway                           { return d.gw }
func (d *mockSessionDeps) GetHistoryManager() ports.HistoryManager              { return d.hManager }
func (d *mockSessionDeps) GetRegistry() tools.Registry                          { return d.reg }
func (d *mockSessionDeps) GetSecurityManager() security.ISecurityManager        { return d.sm }
func (d *mockSessionDeps) GetEventBus() events.EventBus                         { return d.bus }
func (d *mockSessionDeps) GetPaths() *persistence.Paths                         { return d.paths }
func (d *mockSessionDeps) GetPricingOverrides() map[string]pricing.ModelPricing { return nil }
func (d *mockSessionDeps) GetTracker() pricing.CostTracker                      { return d.tracker }
func (d *mockSessionDeps) GetPricingData() pricing.PricingData                  { return pricing.PricingData{} }

type mockGateway struct {
	llm.LLMGateway
}

type mockHistoryManager struct {
	ports.HistoryManager
}

func (m *mockHistoryManager) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

type mockRegistry struct {
	tools.Registry
}

func (m *mockRegistry) Register(tool *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (m *mockRegistry) RegisterWithOptions(tool *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
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

type mockSecurityManager struct {
	security.ISecurityManager
}

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

	deps := &mockSessionDeps{
		gw:       &mockGateway{},
		hManager: &mockHistoryManager{},
		reg:      &mockRegistry{},
		sm:       &mockSecurityManager{},
		bus:      events.NewSimpleEventBus(),
		paths: &persistence.Paths{
			ModeDir: modeDir,
		},
		tracker: &mockTracker{},
	}

	cfg := ports.ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		LogPath:      filepath.Join(tmpDir, "trace.log"),
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
