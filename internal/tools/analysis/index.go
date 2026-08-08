// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"go/token"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/tools/go/packages"
)

// dirLocks provides per-directory mutual exclusion for packages.Load calls.
// Each unique directory gets its own sync.Mutex, allowing parallel loads on
// different directories (which have separate build caches) while serializing
// loads on the same directory (preventing deadlocks on Windows where concurrent
// go subprocess invocations contend for the build cache lock).
var dirLocks sync.Map // map[string]*sync.Mutex

// withDirLock executes fn while holding the mutex associated with dir.
// The dir is normalized to an absolute path for a consistent lock key.
func withDirLock(dir string, fn func()) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir // fallback: still provides the safety property
	}
	v, ok := dirLocks.Load(abs)
	if !ok {
		v, _ = dirLocks.LoadOrStore(abs, &sync.Mutex{})
	}
	v.(*sync.Mutex).Lock()
	defer v.(*sync.Mutex).Unlock()
	fn()
}

// location represents a position in a source file.
type location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Location is the exported alias for location, for use by sub-packages (e.g., analysistest).
type Location = location

// symbolLocation extends location with symbol metadata.
type symbolLocation struct {
	location
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "func", "type", "var", "const"
	Signature string `json:"signature,omitempty"`
	Receiver  string `json:"receiver,omitempty"` // For methods
}

// SymbolLocation is the exported alias for symbolLocation.
type SymbolLocation = symbolLocation

// typeName represents a fully qualified type name.
type typeName struct {
	PkgPath string `json:"pkg_path"`
	Name    string `json:"name"`
}

// TypeName is the exported alias for typeName.
type TypeName = typeName

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
	// WarmImplementations eagerly computes the implementation cache.
	// It calls computeImplementationsLazy() and discards the result,
	// warming the cache as a side effect. Interactive consumers
	// (TUI, long-running agent) opt in; non-interactive consumers (CI)
	// skip it.
	//
	// The context parameter is accepted for future cancellation support
	// but is not yet wired (the underlying sync.Once does not support
	// cancellation). Calling with a cancelled context is safe and will
	// not panic, but the warm-up will proceed regardless.
	//
	// Idempotent: safe to call when the cache is already hot
	// (sync.Once returns immediately).
	WarmImplementations(ctx context.Context)

	// HarvestDeclarations walks all loaded packages and invokes fn for
	// each exported, non-test, non-init symbol. The callback receives a
	// pre-populated symMeta with all evaluation flags set. The indexer
	// implementation populates obj from go/types; fixture implementations
	// may leave obj nil while setting isInterfaceType, isInterfaceMethod,
	// and isWellKnownContract directly.
	//
	// The callback returns bool: false to stop iteration early, true to
	// continue. This follows the standard Go visitor pattern (e.g.,
	// filepath.WalkDir, ast.Inspect).
	HarvestDeclarations(ctx context.Context, fn func(meta *symMeta) bool, hb chan<- struct{}) error

	// Packages returns the loaded packages.
	Packages(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error)
	// Refresh re-scans the workspace to update the index.
	Refresh(ctx context.Context, hb chan<- struct{}) error
}

// SymbolIndex is the exported alias for symbolIndex, for use by sub-packages.
type SymbolIndex = symbolIndex

// Indexer implements SymbolIndex using go/packages and go/types.
type indexer struct {
	dir  string
	fset *token.FileSet
	mu   sync.RWMutex
	pkgs []*packages.Package

	symbolsByPath                  map[string][]symbolLocation
	usagesByName                   map[string][]location
	implsCache                     *implCacheEntry // replaced on each Refresh; cycle-bound to prevent stale writeback
	lastRefresh                    time.Time
	refreshMu                      sync.Mutex // For serializing Refresh calls
	testComputeImplementationsHook func()     // Test hook: nil in production (ADR-032)
	// resolvePath resolves a filename to an absolute path. Override in tests
	// to inject path-resolution errors without OS-specific hacks.
	resolvePath     func(string) (string, error)
	clk             clock.Clock
	knownModulePath string // if set, skip discoverModulePath; zero-value safe
}

// implCacheEntry bundles a sync.Once gate with its computed result.
// Each Refresh cycle allocates a fresh entry, ensuring old computations
// cannot poison the new cycle's cache (see Issue #449).
type implCacheEntry struct {
	once  sync.Once
	impls map[string][]string
}

const refreshTTL = 5 * time.Second

func newIndexer(dir string) (*indexer, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir // fallback
	}
	return &indexer{
		dir:           absDir,
		fset:          token.NewFileSet(),
		symbolsByPath: make(map[string][]symbolLocation),
		usagesByName:  make(map[string][]location),
		implsCache:    &implCacheEntry{},
		resolvePath:   filepath.Abs,
		clk:           clock.RealClock{},
	}, nil
}

// startHeartbeatTicker starts a background goroutine that periodically sends
// heartbeats on hb. It returns a stop function that must be called to clean up.
func startHeartbeatTicker(hb chan<- struct{}, clk clock.Clock) (stop func()) {
	if clk == nil {
		clk = clock.RealClock{}
	}
	done := make(chan struct{})
	go func() {
		ticker := clk.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C():
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

	stop := startHeartbeatTicker(hb, idx.clk)
	defer stop()

	fset := token.NewFileSet()
	pkgs, err := idx.loadPackages(ctx, fset)
	if err != nil {
		return fmt.Errorf("loading packages: %w", err)
	}

	symbolsByPath, usagesByName, err := idx.harvestPackages(ctx, fset, pkgs, hb)
	if err != nil {
		return fmt.Errorf("harvesting packages: %w", err)
	}

	idx.updateState(pkgs, symbolsByPath, usagesByName, fset)
	return nil
}

func (idx *indexer) toLocation(pos token.Pos) location {
	p := idx.fset.Position(pos)
	abs, err := idx.resolvePath(p.Filename)
	if err != nil {
		abs = p.Filename // fallback to raw filename; better than empty string
	}
	return location{
		Path:   abs,
		Line:   p.Line,
		Column: p.Column,
	}
}

func (idx *indexer) Lookup(ctx context.Context, symbol string, hb chan<- struct{}) ([]location, error) {
	if err := idx.Refresh(ctx, hb); err != nil {
		return nil, fmt.Errorf("refreshing index for lookup: %w", err)
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
		return nil, fmt.Errorf("refreshing index for packages: %w", err)
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
	var modulePath string
	if idx.knownModulePath != "" {
		modulePath = idx.knownModulePath
	} else {
		modulePath = idx.discoverModulePath(ctx, fset)
	}

	pattern := "./..."
	if modulePath != "" {
		pattern = modulePath + "/..."
	}

	cfg := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Dir:     idx.dir,
		Fset:    fset,
		Context: ctx,
		// BuildFlags: -tags=arch makes arch-gated consumers visible so
		// arch-only symbols are not mis-flagged DEAD/PRIVATE. The concrete
		// case: loadTransitiveWhitelist's only caller is
		// real_architecture_test.go:66, behind //go:build arch — without
		// this flag the scan reported it DEAD. Only two files in the module
		// are arch-tagged (real_architecture_test.go,
		// real_nonfix_catalog_test.go), both in-package tests; loading them
		// adds usages, never harvestable declarations (test symbols are
		// excluded by HarvestDeclarations' non-test filter).
		BuildFlags: []string{"-tags=arch"},
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
	var pkgs []*packages.Package
	var loadErr error
	withDirLock(idx.dir, func() {
		pkgs, loadErr = packages.Load(cfg, pattern)
	})
	return pkgs, loadErr
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
	var pkgs []*packages.Package
	var loadErr error
	withDirLock(idx.dir, func() {
		pkgs, loadErr = packages.Load(cfg, ".")
	})
	if loadErr != nil || len(pkgs) == 0 {
		log.Printf("analysis: discoverModulePath failed (dir=%s, err=%v, pkgs=%d), falling back to ./... pattern", idx.dir, loadErr, len(pkgs))
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
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pkgs = pkgs
	idx.fset = fset
	idx.symbolsByPath = symbolsByPath
	idx.usagesByName = usagesByName
	idx.implsCache = &implCacheEntry{} // fresh cycle gate; old entry abandoned and GC'd (Issue #449)
	idx.lastRefresh = time.Now()
}

func (idx *indexer) IsSymbolUsed(ctx context.Context, name string, hb chan<- struct{}) bool {
	if err := idx.Refresh(ctx, hb); err != nil {
		log.Printf("analysis: index refresh failed in IsSymbolUsed: %v", err)
		return false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	usages, ok := idx.usagesByName[name]
	return ok && len(usages) > 0
}
