// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"go/token"
	"go/types"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetImplementations_SingleflightCoalescing verifies that
// computeImplementationsLazy coalesces N concurrent GetImplementations
// calls into exactly 1 computeImplementations invocation after a
// refresh invalidates the implementations cache (Issue #359).
func TestGetImplementations_SingleflightCoalescing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}
	t.Parallel()

	// Step 1: Create a workspace with an interface and a concrete implementation.
	// We need valid Go code where a type implements an interface so
	// computeImplementations produces a non-empty map and GetImplementations
	// can return results for a known interface-method ID.
	code := `package test

type Greeter interface {
	Greet() string
}

type EnglishGreeter struct{}

func (e EnglishGreeter) Greet() string { return "hello" }
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	_ = tmpDir

	// Step 2: Discover a valid interface-method ID by computing
	// implementations directly. We use computeImplementationsLazy
	// (same package) to get the full map and pick a key.
	// This also primes the implementations cache (idx.implsCache.impls).
	knownMap := idx.computeImplementationsLazy()
	require.NotEmpty(t, knownMap, "expected at least one implementation from test workspace")

	var queryID string
	for id := range knownMap {
		queryID = id
		break
	}
	require.NotEmpty(t, queryID, "expected a non-empty interface-method ID")

	// Freeze the indexer state so subsequent GetImplementations calls
	// do NOT invoke Refresh → loadPackages → packages.Load.
	// This isolates the coalescing test from external
	// package-load failures.
	idx.mu.Lock()
	idx.lastRefresh = time.Now().Add(1 * time.Hour)
	idx.mu.Unlock()

	// Wire the ADR-032 test hook: a local atomic counter that increments
	// each time computeImplementations is entered. The hook is nil-checked
	// in production (zero overhead) and set only in this test.
	var computeCount atomic.Int64
	idx.testComputeImplementationsHook = func() {
		computeCount.Add(1)
	}

	// Step 3: Invalidate the cache and reset counter
	idx.mu.Lock()
	idx.implsCache = &implCacheEntry{}
	idx.mu.Unlock()
	computeCount.Store(0)

	// Step 4: Deterministic barrier — every goroutine signals readiness
	// before any goroutine proceeds. This eliminates the time.Sleep
	// heuristic that caused flakiness under coverage instrumentation
	// (Issue #427).
	const N = 12
	ready := make(chan struct{}, N) // buffered so goroutines never block on send
	release := make(chan struct{})  // closed to release all goroutines simultaneously
	done := make(chan struct{}, N)  // buffered so goroutines never block on send on completion

	results := make([][]string, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			ready <- struct{}{} // signal: I've reached the barrier
			<-release           // wait: until all N goroutines are present
			results[i] = idx.computeImplementationsLazy()[queryID]
			done <- struct{}{} // signal: I've completed
		}(i)
	}

	// Collect N readiness signals — only after all N have reported
	// are they guaranteed to be at the barrier.
	for i := 0; i < N; i++ {
		<-ready
	}
	// Release all goroutines simultaneously.
	close(release)

	// Wait for all goroutines to complete.
	for i := 0; i < N; i++ {
		<-done
	}

	// Step 5: Assertions — unchanged
	assert.Equal(t, int64(1), computeCount.Load(),
		"sync.Once must coalesce N concurrent calls into exactly 1 compute")
	for i := 0; i < N; i++ {
		assert.NotNil(t, results[i])
	}
	for i := 1; i < N; i++ {
		assert.ElementsMatch(t, results[0], results[i])
	}
}

// TestSatisfiesGenericInterface_EmptyInterface verifies the defensive guard
// that prevents a panic when an empty interface (NumMethods()==0) is passed
// to satisfiesGenericInterface. This is defense-in-depth because
// mapTypeToInterfaces pre-filters interfaces with ptrMethodSetLen < itf.NumMethods(),
// which would reject empty interfaces (0 < 0 is false, but the loop body
// would never execute for empty interfaces anyway). The guard at L74
// ensures the function returns false immediately for empty interfaces.
func TestSatisfiesGenericInterface_EmptyInterface(t *testing.T) {
	t.Parallel()

	idx := &indexer{}
	emptyIface := types.NewInterfaceType(nil, nil) // NumMethods() == 0
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, nil, "MyType", types.NewStruct(nil, nil)),
		types.NewStruct(nil, nil),
		nil,
	)
	pkgTypes := types.NewPackage("example.com/pkg", "pkg")

	got := idx.satisfiesGenericInterface(named, emptyIface, pkgTypes)
	if got {
		t.Error("satisfiesGenericInterface should return false for empty interface")
	}
}

func TestGetImplementations_RefreshError(t *testing.T) {
	t.Parallel()

	// Create indexer pointing to a non-existent directory
	idx, err := newIndexer("/nonexistent/directory/for/testing")
	require.NoError(t, err)

	// GetImplementations calls Refresh, which calls loadPackages,
	// which fails because the directory doesn't exist
	result := idx.GetImplementations(context.Background(), "some.Interface.Method", nil)
	if result != nil {
		t.Errorf("expected nil result on Refresh error, got %v", result)
	}
}

// TestWarmImplementations_WarmsGetImplementations verifies that calling
// WarmImplementations before GetImplementations produces the same results
// as calling GetImplementations alone — no semantic change, just cache warming.
func TestWarmImplementations_WarmsGetImplementations(t *testing.T) {
	t.Parallel()

	code := `package test

type Greeter interface {
	Greet() string
}

type EnglishGreeter struct{}

func (e EnglishGreeter) Greet() string { return "hello" }
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	_ = tmpDir

	// Get the full implementations map via the lazy path to discover a valid key.
	fullImpls := idx.computeImplementationsLazy()
	require.NotEmpty(t, fullImpls, "expected at least one implementation")

	var queryID string
	for id := range fullImpls {
		queryID = id
		break
	}
	require.NotEmpty(t, queryID)

	// Freeze the indexer so Refresh does not trigger loadPackages.
	idx.mu.Lock()
	idx.lastRefresh = time.Now().Add(1 * time.Hour)
	idx.mu.Unlock()

	// Invalidate the cache so we start cold.
	idx.mu.Lock()
	idx.implsCache = &implCacheEntry{}
	idx.mu.Unlock()

	// Warm the cache.
	idx.WarmImplementations(context.Background())

	// Now GetImplementations should return the same result from the warmed cache.
	got := idx.GetImplementations(context.Background(), queryID, nil)
	want := fullImpls[queryID]

	assert.ElementsMatch(t, want, got, "GetImplementations after WarmImplementations must match direct computeImplementationsLazy result")
}

// TestWarmImplementations_Idempotent verifies that calling WarmImplementations
// twice does not double-compute. The sync.Once gate ensures the computation
// runs exactly once even when WarmImplementations is called multiple times.
func TestWarmImplementations_Idempotent(t *testing.T) {
	t.Parallel()

	code := `package test

type Greeter interface {
	Greet() string
}

type EnglishGreeter struct{}

func (e EnglishGreeter) Greet() string { return "hello" }
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	_ = tmpDir

	// Freeze Refresh.
	idx.mu.Lock()
	idx.lastRefresh = time.Now().Add(1 * time.Hour)
	idx.mu.Unlock()

	// Invalidate the cache.
	idx.mu.Lock()
	idx.implsCache = &implCacheEntry{}
	idx.mu.Unlock()

	// Wire the test hook to count computeImplementations invocations.
	var computeCount atomic.Int64
	idx.testComputeImplementationsHook = func() {
		computeCount.Add(1)
	}

	// Call WarmImplementations twice.
	idx.WarmImplementations(context.Background())
	idx.WarmImplementations(context.Background())

	assert.Equal(t, int64(1), computeCount.Load(),
		"WarmImplementations called twice must invoke computeImplementations exactly once (sync.Once)")
}

// TestWarmImplementations_CancelledContext verifies that calling
// WarmImplementations with a cancelled context does not panic.
// The ctx parameter is accepted for future cancellation support
// but is not yet wired (sync.Once does not support cancellation).
func TestWarmImplementations_CancelledContext(t *testing.T) {
	t.Parallel()

	code := `package test

type Greeter interface {
	Greet() string
}

type EnglishGreeter struct{}

func (e EnglishGreeter) Greet() string { return "hello" }
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	_ = tmpDir

	// Freeze Refresh.
	idx.mu.Lock()
	idx.lastRefresh = time.Now().Add(1 * time.Hour)
	idx.mu.Unlock()

	// Invalidate the cache.
	idx.mu.Lock()
	idx.implsCache = &implCacheEntry{}
	idx.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	// Must not panic.
	assert.NotPanics(t, func() {
		idx.WarmImplementations(ctx)
	}, "WarmImplementations with cancelled context must not panic")

	// The cache should still be populated (sync.Once ignores ctx).
	fullImpls := idx.computeImplementationsLazy()
	assert.NotEmpty(t, fullImpls, "cache must be populated even with cancelled context")
}

// TestWarmImplementations_Concurrent verifies that N concurrent
// WarmImplementations calls are coalesced into exactly 1
// computeImplementations invocation via the sync.Once gate.
func TestWarmImplementations_Concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}
	t.Parallel()

	code := `package test

type Greeter interface {
	Greet() string
}

type EnglishGreeter struct{}

func (e EnglishGreeter) Greet() string { return "hello" }
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	_ = tmpDir

	// Freeze Refresh.
	idx.mu.Lock()
	idx.lastRefresh = time.Now().Add(1 * time.Hour)
	idx.mu.Unlock()

	// Invalidate the cache.
	idx.mu.Lock()
	idx.implsCache = &implCacheEntry{}
	idx.mu.Unlock()

	var computeCount atomic.Int64
	idx.testComputeImplementationsHook = func() {
		computeCount.Add(1)
	}

	// Deterministic barrier (same pattern as TestGetImplementations_SingleflightCoalescing).
	const N = 12
	ready := make(chan struct{}, N)
	release := make(chan struct{})
	done := make(chan struct{}, N)

	for i := 0; i < N; i++ {
		go func() {
			ready <- struct{}{}
			<-release
			idx.WarmImplementations(context.Background())
			done <- struct{}{}
		}()
	}

	for i := 0; i < N; i++ {
		<-ready
	}
	close(release)

	for i := 0; i < N; i++ {
		<-done
	}

	assert.Equal(t, int64(1), computeCount.Load(),
		"sync.Once must coalesce N concurrent WarmImplementations into exactly 1 compute")
}
