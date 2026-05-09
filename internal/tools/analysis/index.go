// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/token"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"
)

// location represents a position in a source file.
type location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// symbolLocation extends location with symbol metadata.
type symbolLocation struct {
	location
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "func", "type", "var", "const"
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"` // For methods
}

// typeName represents a fully qualified type name.
type typeName struct {
	PkgPath string `json:"pkg_path"`
	Name    string `json:"name"`
}

// SymbolIndex provides methods to query symbols and their relationships in a Go workspace.
type symbolIndex interface {
	// Lookup returns the locations where the given symbol is defined.
	Lookup(ctx context.Context, symbol string, hb chan<- struct{}) ([]location, error)
	// FindImplementors returns the types that implement the given interface.
	FindImplementors(ctx context.Context, interfaceName string, hb chan<- struct{}) ([]typeName, error)
	// SearchSymbols searches for symbols matching the query in the given path.
	SearchSymbols(ctx context.Context, path string, query string, exportedOnly bool, hb chan<- struct{}) ([]symbolLocation, error)
	// GetUsages returns all locations where the given symbol name is used.
	GetUsages(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]location, error)
	// IsSymbolUsed returns true if the provided name exists in the index with at least one usage.
	IsSymbolUsed(ctx context.Context, name string, hb chan<- struct{}) bool
	// GetImplementations returns the concrete method identities that implement the given interface method.
	GetImplementations(ctx context.Context, interfaceMethodId string, hb chan<- struct{}) []string
	// Packages returns the loaded packages.
	Packages(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error)
	// Refresh re-scans the workspace to update the index.
	Refresh(ctx context.Context, hb chan<- struct{}) error
}

// Indexer implements SymbolIndex using go/packages and go/types.
type indexer struct {
	dir  string
	fset *token.FileSet
	mu   sync.RWMutex
	pkgs []*packages.Package

	symbolsByPath   map[string][]symbolLocation
	usagesByName    map[string][]location
	implementations map[string][]string // interface method id -> concrete method ids
	lastRefresh     time.Time
	refreshMu       sync.Mutex // For serializing Refresh calls
}

const refreshTTL = 5 * time.Second

func newIndexer(dir string) (*indexer, error) {
	return &indexer{
		dir:             dir,
		fset:            token.NewFileSet(),
		symbolsByPath:   make(map[string][]symbolLocation),
		usagesByName:    make(map[string][]location),
		implementations: make(map[string][]string),
	}, nil
}

// startHeartbeatTicker starts a background goroutine that periodically sends
// heartbeats on hb. It returns a stop function that must be called to clean up.
func startHeartbeatTicker(hb chan<- struct{}) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	return func() { close(done) }
}

func (idx *indexer) Refresh(ctx context.Context, hb chan<- struct{}) error {
	if !idx.needsRefresh() {
		return nil
	}

	idx.refreshMu.Lock()
	defer idx.refreshMu.Unlock()

	if !idx.needsRefresh() {
		return nil
	}

	stop := startHeartbeatTicker(hb)
	defer stop()

	fset := token.NewFileSet()
	pkgs, err := idx.loadPackages(ctx, fset)
	if err != nil {
		return err
	}

	symbolsByPath, usagesByName, err := idx.harvestPackages(ctx, fset, pkgs, hb)
	if err != nil {
		return err
	}

	idx.updateState(pkgs, symbolsByPath, usagesByName, fset)
	return nil
}

func (idx *indexer) toLocation(pos token.Pos) location {
	p := idx.fset.Position(pos)
	abs, _ := filepath.Abs(p.Filename)
	return location{
		Path:   abs,
		Line:   p.Line,
		Column: p.Column,
	}
}

func (idx *indexer) Lookup(ctx context.Context, symbol string, hb chan<- struct{}) ([]location, error) {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil, err
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var locations []location
	for _, pkg := range idx.pkgs {
		obj := pkg.Types.Scope().Lookup(symbol)
		if obj != nil {
			locations = append(locations, idx.toLocation(obj.Pos()))
		}
	}
	return locations, nil
}

func (idx *indexer) Packages(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil, err
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.pkgs, nil
}

func (idx *indexer) needsRefresh() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return time.Since(idx.lastRefresh) > refreshTTL
}

func (idx *indexer) loadPackages(ctx context.Context, fset *token.FileSet) ([]*packages.Package, error) {
	// First, discover the module path so we can use a module-qualified
	// pattern (modulePath/...) instead of "./...". The "./..." pattern is
	// restricted to the test's package scope when running inside go test,
	// which prevents architecture verification from seeing cross-package
	// imports in other parts of the module.
	modulePath := idx.discoverModulePath(ctx, fset)

	pattern := "./..."
	if modulePath != "" {
		pattern = modulePath + "/..."
	}

	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Dir:     idx.dir,
		Fset:    fset,
		Context: ctx,
		// Tests: true is REQUIRED. It causes go/packages to load synthesized
		// test packages (foo, foo_test, foo.test variants), which dead_code_graph
		// depends on to detect external _test package consumers. Without this,
		// any symbol consumed only by `package foo_test` files is mis-flagged
		// as DEAD/PRIVATE — a known false-positive class.
		//
		// The contract is pinned by TestIndexerLoadsTestPackages in
		// internal/tools/analysis/dead_code_test_consumer_test.go.
		// Do not flip to false without first updating that test and reviewing
		// every dead_code_graph consumer.
		Tests: true,
	}
	return packages.Load(cfg, pattern)
}

// discoverModulePath performs a lightweight package load to discover the
// Go module path. Returns "" if the module cannot be determined.
func (idx *indexer) discoverModulePath(ctx context.Context, fset *token.FileSet) string {
	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedModule,
		Dir:     idx.dir,
		Fset:    fset,
		Context: ctx,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil || len(pkgs) == 0 {
		return ""
	}
	for _, pkg := range pkgs {
		if pkg.Module != nil && pkg.Module.Path != "" {
			return pkg.Module.Path
		}
	}
	return ""
}

func (idx *indexer) updateState(pkgs []*packages.Package, symbolsByPath map[string][]symbolLocation, usagesByName map[string][]location, fset *token.FileSet) {
	impls := idx.computeImplementations(pkgs)

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pkgs = pkgs
	idx.fset = fset
	idx.symbolsByPath = symbolsByPath
	idx.usagesByName = usagesByName
	idx.implementations = impls
	idx.lastRefresh = time.Now()
}

func (idx *indexer) IsSymbolUsed(ctx context.Context, name string, hb chan<- struct{}) bool {
	if err := idx.Refresh(ctx, hb); err != nil {
		return false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	usages, ok := idx.usagesByName[name]
	return ok && len(usages) > 0
}
