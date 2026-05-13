// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"sync"
	"testing"

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
	ctx := context.Background()

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

	// Step 3: Simulate a post-refresh state by directly invalidating the
	// implementations cache and resetting the compute counter.
	// This mimics what Refresh does (sets implementations to nil in updateState).
	idx.mu.Lock()
	idx.implementations = nil
	idx.mu.Unlock()
	idx.computeCount.Store(0)

	// Step 4: Fire N concurrent GetImplementations calls after the cache
	// invalidation. Singleflight must coalesce all of them into exactly
	// one computeImplementations call.
	const N = 12
	var wg sync.WaitGroup
	results := make([][]string, N)
	barrier := make(chan struct{})

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-barrier // all goroutines released simultaneously
			results[i] = idx.GetImplementations(ctx, queryID, nil)
		}(i)
	}

	close(barrier)
	wg.Wait()

	// Step 5: Assertions.

	// (a) computeImplementations was called exactly once.
	assert.Equal(t, int64(1), idx.computeCount.Load(),
		"singleflight must coalesce N concurrent calls into exactly 1 compute")

	// (b) All N goroutines received a non-nil result.
	for i := 0; i < N; i++ {
		assert.NotNil(t, results[i],
			"goroutine %d received nil result; singleflight may have panicked or returned error", i)
	}

	// (c) All results are identical (deep equal).
	// singleflight.DoChan returns the same result.Val to all callers.
	for i := 1; i < N; i++ {
		assert.ElementsMatch(t, results[0], results[i],
			"goroutine %d result differs from goroutine 0; singleflight may not be coalescing", i)
	}
}
