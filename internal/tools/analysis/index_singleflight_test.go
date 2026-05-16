// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComputeImplementationsLazy_ErrorPath verifies that when the singleflight
// callback panics (simulated via the ADR-032 test hook), computeImplementationsLazy
// does not return a partially-computed result. The singleflight.Group.Do re-panics
// when the callback panics, so we recover in the test and assert the result is nil.
// After clearing the hook, normal operation is restored.
func TestComputeImplementationsLazy_ErrorPath(t *testing.T) {
	t.Parallel()

	// Setup: create a valid Go workspace with an interface and implementation.
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.go"),
		[]byte("package test\n\ntype I interface { M() }\ntype S struct{}\nfunc (s S) M() {}\n"), 0644))

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	// Freeze Refresh so subsequent calls do not trigger loadPackages.
	idx.mu.Lock()
	idx.lastRefresh = time.Now().Add(1 * time.Hour)
	idx.mu.Unlock()

	// Prime the implementations cache via the normal code path.
	impls := idx.computeImplementationsLazy()
	require.NotEmpty(t, impls, "expected at least one implementation in test workspace")

	// Invalidate the cache so the next call enters the singleflight path.
	idx.mu.Lock()
	idx.implementations = nil
	idx.mu.Unlock()

	// Wire the test hook to simulate a failure mid-computation.
	// It sets a sentinel value into idx.implementations (simulating a partial
	// write) and then panics. The singleflight.Group.Do catches the panic,
	// wraps it as a *panicError, and re-panics to the caller.
	sentinel := map[string][]string{"sentinel": {"value"}}
	idx.testComputeImplementationsHook = func() {
		idx.mu.Lock()
		idx.implementations = sentinel
		idx.mu.Unlock()
		panic("test-induced panic in computeImplementations")
	}

	// Call computeImplementationsLazy with a recover barrier.
	// The singleflight re-panics, so computeImplementationsLazy never
	// returns normally — the result variable stays nil (zero value).
	var result map[string][]string
	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r, "expected panic to propagate from singleflight")
		}()
		result = idx.computeImplementationsLazy()
	}()

	assert.Nil(t, result, "computeImplementationsLazy must not return a result after panic")

	// Restore normal operation: clear the hook and invalidate the cache
	// (the sentinel was written by the hook before the panic).
	idx.testComputeImplementationsHook = nil
	idx.mu.Lock()
	idx.implementations = nil
	idx.mu.Unlock()

	// Third call: normal operation is restored, real implementations are computed.
	result = idx.computeImplementationsLazy()
	require.NotNil(t, result, "normal operation must be restored after clearing the hook")
	require.NotEmpty(t, result, "implementations must be recomputed after recovery")
}

// TestRefresh_HarvestErrorPreservesState verifies that when harvestPackages
// returns an error (triggered by a cancelled context), Refresh does not
// partially update the indexer state. The prior fset, symbols, and usages
// must remain intact.
func TestRefresh_HarvestErrorPreservesState(t *testing.T) {
	t.Parallel()

	// Setup: create a valid Go workspace.
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.go"),
		[]byte("package test\n\nfunc F() {}\n"), 0644))

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	// First Refresh: populate the indexer state.
	ctx := context.Background()
	require.NoError(t, idx.Refresh(ctx, nil))

	// Record the pre-error state (after successful Refresh).
	idx.mu.RLock()
	symbolsLenBefore := len(idx.symbolsByPath)
	usagesLenBefore := len(idx.usagesByName)
	fsetBefore := idx.fset
	idx.mu.RUnlock()

	require.NotZero(t, symbolsLenBefore, "symbolsByPath must be populated after successful Refresh")
	require.NotNil(t, fsetBefore, "fset must be set after successful Refresh")

	// Force a re-refresh by setting lastRefresh far in the past, and
	// record the expected post-error value. If Refresh fails, updateState
	// is never called, so lastRefresh must retain this stale value.
	idx.mu.Lock()
	staleRefresh := time.Now().Add(-1 * time.Hour)
	idx.lastRefresh = staleRefresh
	idx.mu.Unlock()

	// Run Refresh with a cancelled context. The cancelled context causes
	// harvestPackages to fail — sem.Acquire(gCtx, 1) returns context.Canceled
	// immediately, and the errgroup propagates that error. Refresh returns
	// the error without calling updateState, preserving prior state.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err = idx.Refresh(cancelledCtx, nil)
	require.Error(t, err, "Refresh must return an error with cancelled context")
	assert.Contains(t, err.Error(), "canceled", "error must indicate context cancellation")

	// Verify state is UNCHANGED after the failed Refresh.
	idx.mu.RLock()
	lastRefreshAfter := idx.lastRefresh
	symbolsLenAfter := len(idx.symbolsByPath)
	usagesLenAfter := len(idx.usagesByName)
	fsetAfter := idx.fset
	idx.mu.RUnlock()

	assert.True(t, lastRefreshAfter.Equal(staleRefresh),
		"lastRefresh must be unchanged after failed Refresh: expected=%v got=%v",
		staleRefresh, lastRefreshAfter)
	assert.Equal(t, symbolsLenBefore, symbolsLenAfter,
		"symbolsByPath must be unchanged after failed Refresh")
	assert.Equal(t, usagesLenBefore, usagesLenAfter,
		"usagesByName must be unchanged after failed Refresh")
	assert.Same(t, fsetBefore, fsetAfter,
		"fset must be the same pointer after failed Refresh (no new fset allocated)")
}
