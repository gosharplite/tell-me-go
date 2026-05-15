// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
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
	// This also primes the implementations cache (idx.implementations).
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
	// This isolates the singleflight coalescing test from external
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
	idx.implementations = nil
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
		"singleflight must coalesce N concurrent calls into exactly 1 compute")
	for i := 0; i < N; i++ {
		assert.NotNil(t, results[i])
	}
	for i := 1; i < N; i++ {
		assert.ElementsMatch(t, results[0], results[i])
	}
}
