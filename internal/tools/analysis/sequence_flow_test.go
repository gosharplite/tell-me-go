// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

func TestResolveStartFunc(t *testing.T) {
	t.Parallel()
	a := newSequenceAnalyzer(nil, nil, nil)

	t.Run("finds function in package", func(t *testing.T) {
		t.Parallel()
		code := `package test
func TargetFunc() {}
func OtherFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		fd, err := a.resolveStartFunc(pkg, "TargetFunc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fd.Name.Name != "TargetFunc" {
			t.Errorf("expected TargetFunc, got %s", fd.Name.Name)
		}
	})

	t.Run("returns error when not found", func(t *testing.T) {
		t.Parallel()
		code := `package test
func OtherFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		_, err := a.resolveStartFunc(pkg, "MissingFunc")
		if err == nil {
			t.Error("expected error for missing function")
		}
	})

	t.Run("resolves method with receiver", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Method() {}
func StandaloneFunc() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		fd, err := a.resolveStartFunc(pkg, "(*T).Method")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fd.Name.Name != "Method" {
			t.Errorf("expected Method, got %s", fd.Name.Name)
		}
	})

	t.Run("falls back to bestMatch when no isMethodMatch succeeds", func(t *testing.T) {
		t.Parallel()
		code := `package test
type T struct{}
func (t *T) Method() {}
`
		fset, f := parseTestFile(t, code)
		_ = fset
		pkg := &packages.Package{
			Syntax: []*ast.File{f},
		}
		fd, err := a.resolveStartFunc(pkg, "Method")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// isMethodMatch fails for a method when remaining has no dot,
		// but bestMatch captures it as fallback
		if fd.Name.Name != "Method" {
			t.Errorf("expected Method, got %s", fd.Name.Name)
		}
		if fd.Recv == nil {
			t.Error("expected method with receiver (bestMatch fallback), got plain function")
		}
	})
}

func TestAnalyzeSequenceFlow_MaxDepthInt(t *testing.T) {
	t.Parallel()
	pkgA, pkgB := setupMockPackages()
	idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgA.PkgPath + ".StartFunc",
		"max_depth":    2,
	}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res.Text == "" {
		t.Error("expected non-empty diagram with max_depth=2 (int)")
	}
}

func TestAnalyzeSequenceFlow_MaxDepthUnexpectedType(t *testing.T) {
	t.Parallel()
	pkgA, pkgB := setupMockPackages()
	idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgA.PkgPath + ".StartFunc",
		"max_depth":    "invalid",
	}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res.Text == "" {
		t.Error("expected non-empty diagram with default depth 5")
	}
}

func TestAnalyzeSequenceFlow_MaxDepthAbsent(t *testing.T) {
	t.Parallel()
	pkgA, pkgB := setupMockPackages()
	idx := &mockIndexer{pkgs: []*packages.Package{pkgA, pkgB}}
	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": pkgA.PkgPath + ".StartFunc",
	}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res.Text == "" {
		t.Error("expected non-empty diagram with default depth 5")
	}
}

func TestAnalyzeSequenceFlow_EmptyStartSymbol(t *testing.T) {
	t.Parallel()
	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, &mockIndexer{})

	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": "",
	}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error: missing") {
		t.Errorf("expected 'Error: missing' in result, got: %s", res.Text)
	}
}

func TestAnalyzeSequenceFlow_TraceFlowError(t *testing.T) {
	t.Parallel()
	idx := &mockIndexer{err: fmt.Errorf("index failure")}
	analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

	res, err := analyzer.AnalyzeSequenceFlow(context.Background(), map[string]interface{}{
		"start_symbol": "example.com/pkg.Func",
	}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Error tracing flow") {
		t.Errorf("expected 'Error tracing flow' in result, got: %s", res.Text)
	}
}

// TestLoadPackages_ErrorPaths exercises error paths in loadPackages:
// indexer failure and empty package list.
func TestLoadPackages_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("indexer error propagates", func(t *testing.T) {
		t.Parallel()
		idx := &mockIndexer{err: fmt.Errorf("boom")}
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)
		// lastLoad is zero, so cache is expired; loadPackages calls idx.Packages.

		err := analyzer.loadPackages(context.Background(), nil)
		if err == nil {
			t.Error("expected error from indexer failure, got nil")
		} else {
			if !strings.Contains(err.Error(), "getting packages from indexer") {
				t.Errorf("expected wrapping message, got: %v", err)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Errorf("expected original error 'boom', got: %v", err)
			}
		}
	})

	t.Run("zero packages returns error", func(t *testing.T) {
		t.Parallel()
		idx := &mockIndexer{pkgs: []*packages.Package{}} // empty slice
		analyzer := newSequenceAnalyzer(&mockExecutor{}, &mockSecurityProvider{}, idx)

		err := analyzer.loadPackages(context.Background(), nil)
		if err == nil {
			t.Error("expected error for empty packages, got nil")
		} else if !strings.Contains(err.Error(), "no packages loaded") {
			t.Errorf("expected 'no packages loaded', got: %v", err)
		}
	})
}

func TestTraceFlow_SymbolNotFound_NoSlash(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{
		pkgs:     []*packages.Package{},
		funcMap:  make(map[string]funcInfo),
		lastLoad: time.Now(),
		cacheTTL: 5 * time.Minute,
	}

	_, err := a.traceFlow(context.Background(), "MissingFunc", 5, nil)
	if err == nil {
		t.Error("expected error, got nil")
	} else {
		if !strings.Contains(err.Error(), "start symbol not found: MissingFunc") {
			t.Errorf("expected 'start symbol not found', got: %v", err)
		}
		if strings.Contains(err.Error(), "try ") {
			t.Errorf("expected no hint for symbol without slash, got: %v", err)
		}
	}
}

func TestTraceFlow_SymbolNotFound_WithSlash_ModulePrefix(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{
		pkgs:     []*packages.Package{},
		funcMap:  make(map[string]funcInfo),
		modName:  "example.com/mod",
		lastLoad: time.Now(),
		cacheTTL: 5 * time.Minute,
	}

	_, err := a.traceFlow(context.Background(), "example.com/mod/pkg.Func", 5, nil)
	if err == nil {
		t.Error("expected error, got nil")
	} else {
		if !strings.Contains(err.Error(), "start symbol not found") {
			t.Errorf("expected 'start symbol not found', got: %v", err)
		}
		if !strings.Contains(err.Error(), "try 'pkg/path.(*Type).Method'") {
			t.Errorf("expected module-relative hint, got: %v", err)
		}
	}
}

func TestTraceFlow_SymbolNotFound_WithSlash_NoModulePrefix(t *testing.T) {
	t.Parallel()
	a := &defaultSequenceAnalyzer{
		pkgs:     []*packages.Package{},
		funcMap:  make(map[string]funcInfo),
		modName:  "",
		lastLoad: time.Now(),
		cacheTTL: 5 * time.Minute,
	}

	_, err := a.traceFlow(context.Background(), "github.com/foo/pkg.Func", 5, nil)
	if err == nil {
		t.Error("expected error, got nil")
	} else {
		if !strings.Contains(err.Error(), "start symbol not found") {
			t.Errorf("expected 'start symbol not found', got: %v", err)
		}
		if !strings.Contains(err.Error(), "try the full module path") {
			t.Errorf("expected full-module-path hint, got: %v", err)
		}
	}
}

func TestTraceFlow_ResolveStartFuncError(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	code := `package p
func OtherFunc() {}`
	f, _ := parser.ParseFile(fset, "test.go", code, 0)

	pkg := &packages.Package{
		PkgPath: "example.com/pkg",
		Syntax:  []*ast.File{f},
	}

	a := &defaultSequenceAnalyzer{
		pkgs:     []*packages.Package{pkg},
		funcMap:  make(map[string]funcInfo),
		lastLoad: time.Now(),
		cacheTTL: 5 * time.Minute,
	}

	_, err := a.traceFlow(context.Background(), "example.com/pkg.WrongName", 5, nil)
	if err == nil {
		t.Error("expected error, got nil")
	} else {
		if !strings.Contains(err.Error(), "start symbol not found") {
			t.Errorf("expected 'start symbol not found', got: %v", err)
		}
		if !strings.Contains(err.Error(), "WrongName") {
			t.Errorf("expected 'WrongName' in error, got: %v", err)
		}
	}
}
