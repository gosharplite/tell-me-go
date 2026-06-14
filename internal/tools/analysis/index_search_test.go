package analysis

import (
	"context"
	"errors"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindImplementors_RefreshFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		dir:           "/nonexistent/for/implementors",
		fset:          token.NewFileSet(),
		resolvePath:   filepath.Abs,
		implsCache:    &implCacheEntry{},
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
	}

	ctx := context.Background()
	result, err := idx.FindImplementors(ctx, "SomeInterface", nil)
	if err == nil {
		t.Fatal("expected error from FindImplementors with non-existent dir, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}

	want := "refreshing index for implementors"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected error to contain %q, got %q", want, err.Error())
	}
}

func TestIsInSearchPath(t *testing.T) {
	t.Parallel()

	idx := &indexer{} // isInSearchPath is a pure function, no fields needed

	winRoot := filepath.FromSlash("C:/")
	winChild := filepath.FromSlash("C:/a")
	winFile := filepath.FromSlash("C:/a/b.go")
	winSibling := filepath.FromSlash("C:/foobar.go")

	tests := []struct {
		name         string
		target, file string
		want         bool
	}{
		{"exact match", filepath.FromSlash("/a/b.go"), filepath.FromSlash("/a/b.go"), true},
		{"child in subdirectory", filepath.FromSlash("/a"), filepath.FromSlash("/a/b.go"), true},
		{"sibling prefix rejected", filepath.FromSlash("/foo"), filepath.FromSlash("/foobar.go"), false},
		{"root directory", filepath.FromSlash("/"), filepath.FromSlash("/a/b.go"), true},
		{"empty target matches all", "", filepath.FromSlash("/a/b.go"), true},
		{"unrelated path", filepath.FromSlash("/x"), filepath.FromSlash("/y/b.go"), false},
		{"Win root", winRoot, winFile, true},
		{"Win child", winChild, winFile, true},
		{"Win sibling rejected", winChild, winSibling, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idx.isInSearchPath(tt.target, tt.file)
			if got != tt.want {
				t.Errorf("isInSearchPath(%q, %q) = %v; want %v", tt.target, tt.file, got, tt.want)
			}
		})
	}
}

func TestSearchSymbols_AbsFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		fset:        token.NewFileSet(),
		lastRefresh: time.Now(), // make needsRefresh() return false, so Refresh is a no-op
		resolvePath: func(s string) (string, error) {
			return "", errors.New("injected path resolution failure")
		},
	}

	ctx := context.Background()
	results, err := idx.SearchSymbols(ctx, "any", "Anything", false, nil)
	if err == nil {
		t.Fatal("expected error from SearchSymbols with failing resolvePath, got nil")
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}

	want := "resolving search path"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected error to contain %q, got %q", want, err.Error())
	}
}

func TestSearchSymbols_RefreshFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		dir:           "/nonexistent/for/search",
		fset:          token.NewFileSet(),
		resolvePath:   filepath.Abs,
		implsCache:    &implCacheEntry{},
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
	}

	ctx := context.Background()
	result, err := idx.SearchSymbols(ctx, ".", "Anything", false, nil)
	if err == nil {
		t.Fatal("expected error from SearchSymbols with non-existent dir, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}

	want := "refreshing index for search"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected error to contain %q, got %q", want, err.Error())
	}
}

func TestGetUsages_RefreshFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		dir:           "/nonexistent/for/usages",
		fset:          token.NewFileSet(),
		resolvePath:   filepath.Abs,
		implsCache:    &implCacheEntry{},
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
	}

	ctx := context.Background()
	result, err := idx.GetUsages(ctx, "SomeFunc", ".", nil)
	if err == nil {
		t.Fatal("expected error from GetUsages with non-existent dir, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}

	want := "refreshing index for usages"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected error to contain %q, got %q", want, err.Error())
	}
}

func TestGetUsages_AbsFails(t *testing.T) {
	t.Parallel()

	idx := &indexer{
		fset:        token.NewFileSet(),
		lastRefresh: time.Now(), // make needsRefresh() return false, so Refresh is a no-op
		resolvePath: func(s string) (string, error) {
			return "", errors.New("injected path resolution failure")
		},
	}

	ctx := context.Background()
	results, err := idx.GetUsages(ctx, "SomeFunc", "any/path", nil)
	if err == nil {
		t.Fatal("expected error from GetUsages with failing resolvePath, got nil")
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}

	want := "resolving search path"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected error to contain %q, got %q", want, err.Error())
	}
}
