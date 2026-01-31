// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestSetupRegistry_IncludesRestoredTools(t *testing.T) {
	app := New("test")
	cfg := &config.Config{
		Model: "test-model",
		Mode:  "test-mode",
	}
	tmpDir := t.TempDir()
	paths := &sessionPaths{
		modeDir: tmpDir,
		logPath: filepath.Join(tmpDir, "tokens.log"),
	}
	pricingOverrides := make(map[string]types.ModelPricing)

	reg := app.setupRegistry(nil, cfg, paths, pricingOverrides)

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
		if !found {
			t.Errorf("Expected tool %q not found in registry", expected)
		}
	}
}
