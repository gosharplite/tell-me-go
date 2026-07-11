// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// ---------------------------------------------------------------------------
// fixtureIndexer — symbolIndex implementation for test fixtures
// ---------------------------------------------------------------------------

// fixtureIndexer implements symbolIndex from a pre-built indexSnapshot.
// It bypasses packages.Load entirely — all data is served from the snapshot.
// Used in tests to avoid the expensive go/packages type-checking step.
type fixtureIndexer struct {
	mu       sync.RWMutex
	snapshot *indexSnapshot

	// Pre-computed lookup maps for O(1) access.
	declsByID map[string]*symMeta
	pkgPaths  map[string]bool
}

// Compile-time interface check.
var _ SymbolIndex = (*fixtureIndexer)(nil)

// newFixtureIndexer creates a fixtureIndexer from an indexSnapshot.
// It pre-computes lookup maps for O(1) access during analysis.
func newFixtureIndexer(s *indexSnapshot) *fixtureIndexer {
	fi := &fixtureIndexer{
		snapshot:  s,
		declsByID: make(map[string]*symMeta, len(s.Declarations)),
		pkgPaths:  make(map[string]bool, len(s.Declarations)),
	}
	for _, decl := range s.Declarations {
		fi.declsByID[decl.id] = decl
		fi.pkgPaths[decl.pkgPath] = true
	}
	return fi
}

func (fi *fixtureIndexer) Refresh(ctx context.Context, hb chan<- struct{}) error {
	return nil // Fixture is immutable; no-op.
}

func (fi *fixtureIndexer) Lookup(ctx context.Context, symbol string, hb chan<- struct{}) ([]location, error) {
	return nil, nil
}

func (fi *fixtureIndexer) FindImplementors(ctx context.Context, interfaceName string, hb chan<- struct{}) ([]typeName, error) {
	return nil, nil
}

func (fi *fixtureIndexer) SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool, hb chan<- struct{}) ([]symbolLocation, error) {
	return nil, nil
}

func (fi *fixtureIndexer) GetUsages(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]location, error) {
	fi.mu.RLock()
	defer fi.mu.RUnlock()

	usages, ok := fi.snapshot.UsagesByName[symbol]
	if !ok {
		return nil, nil
	}
	if path == "" {
		return usages, nil
	}

	// Filter by search path prefix.
	var filtered []location
	for _, loc := range usages {
		if len(loc.Path) >= len(path) && loc.Path[:len(path)] == path {
			filtered = append(filtered, loc)
		}
	}
	return filtered, nil
}

func (fi *fixtureIndexer) IsSymbolUsed(ctx context.Context, name string, hb chan<- struct{}) bool {
	fi.mu.RLock()
	defer fi.mu.RUnlock()
	usages, ok := fi.snapshot.UsagesByName[name]
	return ok && len(usages) > 0
}

func (fi *fixtureIndexer) GetImplementations(ctx context.Context, interfaceMethodId string, hb chan<- struct{}) []string {
	fi.mu.RLock()
	defer fi.mu.RUnlock()
	return fi.snapshot.ImplsCache[interfaceMethodId]
}

func (fi *fixtureIndexer) Packages(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
	fi.mu.RLock()
	defer fi.mu.RUnlock()

	// Build minimal *packages.Package values from the snapshot.
	// The dead_code pipeline only needs:
	//   - PkgPath        (for buildFileToPkgMap, isInTargetScope, identifyModule)
	//   - Module.Path    (for isInTargetScope, identifyModule)
	//   - GoFiles        (for buildFileToPkgMap, isInTargetScope, hasTextMatchOutsidePackage)
	//
	// TypesInfo, Syntax, Fset, Types are left nil. Downstream code
	// that accesses them (complexity/impact, cross-package AST walk)
	// has nil guards and gracefully degrades.
	pkgFiles := make(map[string][]string, len(fi.snapshot.FileToPkg))
	for file, pkgPath := range fi.snapshot.FileToPkg {
		pkgFiles[pkgPath] = append(pkgFiles[pkgPath], file)
	}

	var pkgs []*packages.Package
	for pkgPath, files := range pkgFiles {
		pkgs = append(pkgs, &packages.Package{
			PkgPath: pkgPath,
			Module: &packages.Module{
				Path: fi.snapshot.ModulePath,
			},
			GoFiles: files,
		})
	}
	return pkgs, nil
}

func (fi *fixtureIndexer) WarmImplementations(ctx context.Context) {
	// Fixture impls are pre-computed in the snapshot; no-op.
}

func (fi *fixtureIndexer) HarvestDeclarations(ctx context.Context, fn func(meta *symMeta) bool, hb chan<- struct{}) error {
	fi.mu.RLock()
	defer fi.mu.RUnlock()

	for _, decl := range fi.snapshot.Declarations {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !fn(decl) {
			return nil
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFixtureIndexer_ConstructAndHarvest(t *testing.T) {
	t.Parallel()

	snap := &indexSnapshot{
		ModulePath: "example.com/test",
		Declarations: []*symMeta{
			{id: "example.com/test/pkg.Foo", pkgPath: "example.com/test/pkg", name: "Foo", symType: "Function"},
			{id: "example.com/test/pkg.Bar", pkgPath: "example.com/test/pkg", name: "Bar", symType: "Type", isInterfaceType: true},
		},
		FileToPkg: map[string]string{
			"/tmp/pkg/file.go": "example.com/test/pkg",
		},
		SymbolsByPath: map[string][]symbolLocation{},
		UsagesByName: map[string][]location{
			"example.com/test/pkg.Foo": {{Path: "/tmp/main.go", Line: 10, Column: 5}},
		},
		ImplsCache: map[string][]string{},
	}

	fi := newFixtureIndexer(snap)

	// Test HarvestDeclarations
	var count int
	err := fi.HarvestDeclarations(context.Background(), func(meta *symMeta) bool {
		count++
		return true
	}, nil)
	if err != nil {
		t.Fatalf("HarvestDeclarations: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 declarations, got %d", count)
	}

	// Test IsSymbolUsed
	if !fi.IsSymbolUsed(context.Background(), "example.com/test/pkg.Foo", nil) {
		t.Error("Foo should be marked as used")
	}
	if fi.IsSymbolUsed(context.Background(), "example.com/test/pkg.Bar", nil) {
		t.Error("Bar should NOT be marked as used (no usages)")
	}

	// Test GetUsages
	usages, err := fi.GetUsages(context.Background(), "example.com/test/pkg.Foo", "", nil)
	if err != nil {
		t.Fatalf("GetUsages: %v", err)
	}
	if len(usages) != 1 || usages[0].Line != 10 {
		t.Errorf("expected 1 usage at line 10, got %v", usages)
	}

	// Test Packages
	pkgs, err := fi.Packages(context.Background(), nil)
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if len(pkgs) != 1 {
		t.Errorf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].PkgPath != "example.com/test/pkg" {
		t.Errorf("expected pkgPath 'example.com/test/pkg', got %q", pkgs[0].PkgPath)
	}
}

func TestIndexSnapshot_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	snap := &indexSnapshot{
		ModulePath: "example.com/test",
		Declarations: []*symMeta{
			{id: "example.com/test/pkg.Foo", pkgPath: "example.com/test/pkg", name: "Foo", symType: "Function"},
		},
		FileToPkg:    map[string]string{"/tmp/pkg/file.go": "example.com/test/pkg"},
		UsagesByName: map[string][]location{"example.com/test/pkg.Foo": {{Path: "/tmp/main.go", Line: 10}}},
		ImplsCache:   map[string][]string{},
	}

	// Save to temp file.
	tmpPath := filepath.Join(t.TempDir(), "snap.json")
	if err := snap.saveSnapshot(tmpPath); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}

	// Load back.
	loaded, err := loadSnapshot(tmpPath)
	if err != nil {
		t.Fatalf("loadSnapshot: %v", err)
	}

	if len(loaded.Declarations) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(loaded.Declarations))
	}
	if loaded.Declarations[0].id != "example.com/test/pkg.Foo" {
		t.Errorf("expected id 'example.com/test/pkg.Foo', got %q", loaded.Declarations[0].id)
	}
	if loaded.ModulePath != "example.com/test" {
		t.Errorf("expected module path 'example.com/test', got %q", loaded.ModulePath)
	}
}
