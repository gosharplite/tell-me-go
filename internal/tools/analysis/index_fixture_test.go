// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFixtureIndexer_ConstructAndHarvest(t *testing.T) {
	t.Parallel()

	snap := &IndexSnapshot{
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

	snap := &IndexSnapshot{
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
	if err := snap.SaveSnapshot(tmpPath); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	// Load back.
	loaded, err := LoadSnapshot(tmpPath)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
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
