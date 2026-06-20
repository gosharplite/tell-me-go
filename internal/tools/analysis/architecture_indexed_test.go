// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestIndexedPackageProvider_LoadPackages(t *testing.T) {
	t.Parallel()
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
		t.Parallel()
		provider.idx = &mockIndexer{pkgs: nil}
		_, err := provider.LoadPackages(ctx)
		if err == nil || err.Error() != "no packages found in index" {
			t.Errorf("expected error for empty index, got %v", err)
		}
	})
}

func TestCollectTrackedImports(t *testing.T) {
	t.Parallel()

	m := &architectureManager{
		ModulePath: "github.com/gosharplite/tell-me-go",
	}
	p := &indexedPackageProvider{m: m}

	t.Run("tracked package with mixed imports", func(t *testing.T) {
		t.Parallel()
		pkg := &packages.Package{
			PkgPath: "github.com/gosharplite/tell-me-go/internal/domain",
			Imports: map[string]*packages.Package{
				"github.com/gosharplite/tell-me-go/internal/agent":  {},
				"github.com/gosharplite/tell-me-go/internal/domain": {},
				"fmt":                            {},
				"golang.org/x/tools/go/packages": {},
			},
		}
		got := p.collectTrackedImports(pkg)
		if len(got) != 2 {
			t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
		}
	})

	t.Run("non-tracked package returns nil", func(t *testing.T) {
		t.Parallel()
		pkg := &packages.Package{
			PkgPath: "fmt",
			Imports: map[string]*packages.Package{
				"errors": {},
			},
		}
		got := p.collectTrackedImports(pkg)
		if got != nil {
			t.Fatalf("expected nil for non-tracked package, got %v", got)
		}
	})

	t.Run("tracked package with no internal imports", func(t *testing.T) {
		t.Parallel()
		pkg := &packages.Package{
			PkgPath: "github.com/gosharplite/tell-me-go/internal/domain",
			Imports: map[string]*packages.Package{
				"fmt":     {},
				"strings": {},
			},
		}
		got := p.collectTrackedImports(pkg)
		if got == nil {
			t.Fatal("expected non-nil slice for tracked package")
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 imports, got %d: %v", len(got), got)
		}
	})
}

func TestIndexedPackageProvider_DetectModuleFromPackages(t *testing.T) {
	t.Parallel()

	t.Run("sets ModulePath from first package with Module", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			ModulePath: "",
		}
		p := &indexedPackageProvider{
			m: m,
		}
		pkgs := []*packages.Package{
			{
				PkgPath: "example.com/project/internal/foo",
				Module:  &packages.Module{Path: "example.com/project"},
			},
		}
		p.detectModuleFromPackages(pkgs)
		if m.ModulePath != "example.com/project" {
			t.Errorf("expected ModulePath 'example.com/project', got %q", m.ModulePath)
		}
	})

	t.Run("picks first package when multiple have Module", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			ModulePath: "",
		}
		p := &indexedPackageProvider{
			m: m,
		}
		pkgs := []*packages.Package{
			{
				PkgPath: "example.com/first/internal/foo",
				Module:  &packages.Module{Path: "example.com/first"},
			},
			{
				PkgPath: "example.com/second/internal/bar",
				Module:  &packages.Module{Path: "example.com/second"},
			},
		}
		p.detectModuleFromPackages(pkgs)
		if m.ModulePath != "example.com/first" {
			t.Errorf("expected ModulePath from first package, got %q", m.ModulePath)
		}
	})

	t.Run("skips packages with nil Module", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			ModulePath: "",
		}
		p := &indexedPackageProvider{
			m: m,
		}
		pkgs := []*packages.Package{
			{
				PkgPath: "example.com/project/internal/foo",
				Module:  nil,
			},
			{
				PkgPath: "example.com/project/internal/bar",
				Module:  &packages.Module{Path: "example.com/project"},
			},
		}
		p.detectModuleFromPackages(pkgs)
		if m.ModulePath != "example.com/project" {
			t.Errorf("expected ModulePath from second package, got %q", m.ModulePath)
		}
	})

	t.Run("no packages with Module leaves ModulePath unchanged", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			ModulePath: "",
		}
		p := &indexedPackageProvider{
			m: m,
		}
		pkgs := []*packages.Package{
			{
				PkgPath: "example.com/project/internal/foo",
				Module:  nil,
			},
		}
		p.detectModuleFromPackages(pkgs)
		if m.ModulePath != "" {
			t.Errorf("expected ModulePath to remain empty, got %q", m.ModulePath)
		}
	})

	t.Run("sync.Once prevents overwrite on second call", func(t *testing.T) {
		t.Parallel()
		m := &architectureManager{
			ModulePath: "",
		}
		p := &indexedPackageProvider{
			m: m,
		}
		pkgs1 := []*packages.Package{
			{
				PkgPath: "example.com/first/internal/foo",
				Module:  &packages.Module{Path: "example.com/first"},
			},
		}
		pkgs2 := []*packages.Package{
			{
				PkgPath: "example.com/second/internal/bar",
				Module:  &packages.Module{Path: "example.com/second"},
			},
		}
		p.detectModuleFromPackages(pkgs1)
		p.detectModuleFromPackages(pkgs2) // should be no-op due to sync.Once
		if m.ModulePath != "example.com/first" {
			t.Errorf("sync.Once should prevent overwrite, got %q", m.ModulePath)
		}
	})
}
