// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

// newWalkGuardAnalyzer returns a fully initialized analyzer and frame collector
// suitable for direct walk() calls in guard-clause tests.
func newWalkGuardAnalyzer() (*defaultSequenceAnalyzer, *frameCollector) {
	a := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, &mockIndexer{})
	return a, &frameCollector{}
}

// setupWalkGuardTest calls setupMockPackages and returns the package
// and the *ast.FuncDecl matching funcName. It fails the test via t.Fatal
// if the function is not found.
func setupWalkGuardTest(t *testing.T, funcName string) (*packages.Package, *ast.FuncDecl) {
	t.Helper()
	pkgA, _ := setupMockPackages()
	for _, decl := range pkgA.Syntax[0].Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == funcName {
			return pkgA, fd
		}
	}
	t.Fatalf("function %q not found in mock package", funcName)
	return nil, nil
}

// TestSequenceAnalyzer_WalkGuards exercises every guard clause in walk(...)
// by calling it directly (not through AnalyzeSequenceFlow).
func TestSequenceAnalyzer_WalkGuards(t *testing.T) {
	t.Parallel()

	t.Run("depth at maxDepth returns early", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := make(map[string]bool)

		a.walk(context.Background(), pkgA, startFunc, 5, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("nil function body returns early", func(t *testing.T) {
		t.Parallel()
		fd := parseFuncDecl(t, "package p; func External()")

		pkg := &packages.Package{
			TypesInfo: &types.Info{Defs: make(map[*ast.Ident]types.Object)},
		}

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := make(map[string]bool)

		a.walk(context.Background(), pkg, fd, 0, 5, frames, visited, "", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("nil def object returns early", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		pkg := &packages.Package{
			PkgPath:   pkgA.PkgPath,
			Syntax:    pkgA.Syntax,
			TypesInfo: &types.Info{Defs: make(map[*ast.Ident]types.Object)},
			Imports:   pkgA.Imports,
			Module:    pkgA.Module,
		}

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := make(map[string]bool)

		a.walk(context.Background(), pkg, startFunc, 0, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("already visited returns early", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		obj := pkgA.TypesInfo.Defs[startFunc.Name]
		if obj == nil {
			t.Fatal("StartFunc object not found in TypesInfo")
		}
		key := getSymbolIdentity(obj)

		a := &defaultSequenceAnalyzer{}
		frames := &frameCollector{}
		visited := map[string]bool{key: true}

		a.walk(context.Background(), pkgA, startFunc, 0, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) != 0 {
			t.Errorf("expected 0 frames, got %d", len(frames.frames))
		}
	})

	t.Run("successful walk adds frames", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		a, frames := newWalkGuardAnalyzer()
		visited := make(map[string]bool)

		a.walk(context.Background(), pkgA, startFunc, 0, 5, frames, visited, "github.com/test/mod", nil)

		if len(frames.frames) == 0 {
			t.Error("expected frames to be added, got none")
		}
	})

	t.Run("heartbeat channel receives when non-nil", func(t *testing.T) {
		t.Parallel()
		pkgA, startFunc := setupWalkGuardTest(t, "StartFunc")

		a, frames := newWalkGuardAnalyzer()
		visited := make(map[string]bool)
		hb := make(chan struct{}, 1)

		a.walk(context.Background(), pkgA, startFunc, 0, 5, frames, visited, "github.com/test/mod", hb)

		if len(hb) != 1 {
			t.Errorf("expected 1 heartbeat, got %d", len(hb))
		}
	})
}
