// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestIndexedPackageProvider_LoadPackages(t *testing.T) {
	ctx := context.Background()
	m := &architectureManager{
		ModulePath: "", // Should be populated by LoadPackages
	}

	// Mock package with internal imports
	pkg := &packages.Package{
		PkgPath: "github.com/gosharplite/tell-me-go/internal/domain",
		Module: &packages.Module{
			Path: "github.com/gosharplite/tell-me-go",
		},
		Imports: map[string]*packages.Package{
			"github.com/gosharplite/tell-me-go/internal/agent": {},
			"fmt": {}, // Should be filtered out (external)
		},
	}

	mockIdx := &mockIndexer{
		pkgs: []*packages.Package{pkg},
	}
	provider := &indexedPackageProvider{m: m, idx: mockIdx}

	pkgs, err := provider.LoadPackages(ctx)
	if err != nil {
		t.Fatalf("LoadPackages failed: %v", err)
	}

	// Verification 1: ModulePath initialization
	if m.ModulePath != "github.com/gosharplite/tell-me-go" {
		t.Errorf("expected ModulePath to be set, got %q", m.ModulePath)
	}

	// Verification 2: Package tracking
	deps, ok := pkgs["github.com/gosharplite/tell-me-go/internal/domain"]
	if !ok {
		t.Fatal("expected package not found in result")
	}

	// Verification 3: Import filtering (only internal module imports)
	if len(deps) != 1 || deps[0] != "github.com/gosharplite/tell-me-go/internal/agent" {
		t.Errorf("unexpected imports: %v", deps)
	}

	t.Run("empty index", func(t *testing.T) {
		provider.idx = &mockIndexer{pkgs: nil}
		_, err := provider.LoadPackages(ctx)
		if err == nil || err.Error() != "no packages found in index" {
			t.Errorf("expected error for empty index, got %v", err)
		}
	})
}
