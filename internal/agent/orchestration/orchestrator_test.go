// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/require"
)

func TestSetupRegistry_IncludesRestoredTools(t *testing.T) {
	tmpDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	orch := &Orchestrator{
		HomeDir: tmpDir,
		SM:      sm,
	}
	cfg := &config.Config{
		Model: "test-model",
		Mode:  "test-mode",
	}
	paths := &persistence.Paths{
		ModeDir:         tmpDir,
		LogPath:         filepath.Join(tmpDir, "tokens.log"),
		CommandsLogPath: filepath.Join(tmpDir, "commands.log"),
	}
	pricingOverrides := make(map[string]domain_pricing.ModelPricing)

	bus := events.NewSimpleEventBus()
	// No shutdown needed for simple bus in this test as it doesn't start goroutines

	reg := orch.SetupRegistry(nil, cfg, paths, pricingOverrides, bus)

	declarations := reg.GetDeclarations()

	expectedTools := []string{
		"estimate_cost",
		"get_cost_summary",
		"verify_release_readiness",
	}

	for _, expected := range expectedTools {
		found := false
		for _, decl := range declarations {
			if decl.Name == expected {
				found = true
				break
			}
		}
		require.True(t, found, "Expected tool %q not found in registry", expected)
	}
}
