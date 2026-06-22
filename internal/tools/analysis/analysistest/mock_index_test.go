// Copyright (c) 2026 gosharplite@gmail.com.
// SPDX-License-Identifier: MIT

package analysistest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"golang.org/x/tools/go/packages"
)

func TestMockSymbolIndex_Lookup(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{}
	locs, err := mock.Lookup(context.Background(), "anySymbol", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if locs != nil {
		t.Errorf("got %v; want nil", locs)
	}
}

func TestMockSymbolIndex_FindImplementors(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{}
	types, err := mock.FindImplementors(context.Background(), "anyInterface", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if types != nil {
		t.Errorf("got %v; want nil", types)
	}
}

func TestMockSymbolIndex_SearchSymbols(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{}
	syms, err := mock.SearchSymbols(context.Background(), "/path", "query", false, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syms != nil {
		t.Errorf("got %v; want nil", syms)
	}
}

func TestMockSymbolIndex_GetUsages_NilFunc(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{} // GetUsagesFunc is nil
	locs, err := mock.GetUsages(context.Background(), "anySymbol", "/path", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if locs != nil {
		t.Errorf("got %v; want nil", locs)
	}
}

func TestMockSymbolIndex_Packages_NilFunc(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{} // PackagesFunc is nil
	pkgs, err := mock.Packages(context.Background(), nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkgs != nil {
		t.Errorf("got %v; want nil", pkgs)
	}
}

func TestMockSymbolIndex_Refresh(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{}
	err := mock.Refresh(context.Background(), nil)

	if err != nil {
		t.Errorf("Refresh() = %v; want nil", err)
	}
}

func TestMockSymbolIndex_WarmImplementations(t *testing.T) {
	t.Parallel()

	mock := &MockSymbolIndex{}
	// Must not panic. The method is a no-op on the mock.
	mock.WarmImplementations(context.Background())
}

func TestMockSymbolIndex_IsSymbolUsed(t *testing.T) {
	t.Parallel()

	t.Run("nil_func_default_false", func(t *testing.T) {
		t.Parallel()
		mock := &MockSymbolIndex{}
		if mock.IsSymbolUsed(context.Background(), "anyName", nil) {
			t.Error("expected false when IsSymbolUsedFunc is nil")
		}
	})

	t.Run("with_func", func(t *testing.T) {
		t.Parallel()
		mock := &MockSymbolIndex{
			IsSymbolUsedFunc: func(ctx context.Context, name string, hb chan<- struct{}) bool {
				return name == "known"
			},
		}
		if !mock.IsSymbolUsed(context.Background(), "known", nil) {
			t.Error("expected true for 'known'")
		}
		if mock.IsSymbolUsed(context.Background(), "unknown", nil) {
			t.Error("expected false for 'unknown'")
		}
	})
}

func TestMockSymbolIndex_GetImplementations(t *testing.T) {
	t.Parallel()

	t.Run("nil_func_default_nil", func(t *testing.T) {
		t.Parallel()
		mock := &MockSymbolIndex{}
		impls := mock.GetImplementations(context.Background(), "anyMethod", nil)
		if impls != nil {
			t.Errorf("got %v; want nil", impls)
		}
	})

	t.Run("with_func", func(t *testing.T) {
		t.Parallel()
		mock := &MockSymbolIndex{
			GetImplementationsFunc: func(ctx context.Context, id string) []string {
				return []string{"impl1", "impl2"}
			},
		}
		impls := mock.GetImplementations(context.Background(), "methodID", nil)
		if len(impls) != 2 {
			t.Fatalf("got %d implementations; want 2", len(impls))
		}
		if impls[0] != "impl1" || impls[1] != "impl2" {
			t.Errorf("got %v; want [impl1 impl2]", impls)
		}
	})
}

func TestMockSymbolIndex_GetUsages_WithFunc(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("usage scan failed")
	mock := &MockSymbolIndex{
		GetUsagesFunc: func(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]analysis.Location, error) {
			return nil, wantErr
		},
	}
	locs, err := mock.GetUsages(context.Background(), "symbol", "/path", nil)

	if err != wantErr {
		t.Errorf("got error %v; want %v", err, wantErr)
	}
	if locs != nil {
		t.Errorf("got %v; want nil on error", locs)
	}
}

func TestMockSymbolIndex_Packages_WithFunc(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("package load failed")
	mock := &MockSymbolIndex{
		PackagesFunc: func(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
			return nil, wantErr
		},
	}
	pkgs, err := mock.Packages(context.Background(), nil)

	if err != wantErr {
		t.Errorf("got error %v; want %v", err, wantErr)
	}
	if pkgs != nil {
		t.Errorf("got %v; want nil on error", pkgs)
	}
}
