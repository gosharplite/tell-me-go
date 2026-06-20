// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package concurrency

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestOnceWithRetry_Success — Single call succeeds, function executes exactly
// once. A second Do() returns nil without re-executing the function.
// ---------------------------------------------------------------------------

func TestOnceWithRetry_Success(t *testing.T) {
	var o OnceWithRetry
	var calls atomic.Int32

	err := o.Do(func() error {
		calls.Add(1)
		return nil
	})

	if err != nil {
		t.Fatalf("first Do() returned error: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 call, got %d", n)
	}
	if !o.done.Load() {
		t.Error("expected done to be true after successful Do")
	}

	// Second call — fast path, function must NOT execute
	err = o.Do(func() error {
		calls.Add(1)
		return nil
	})

	if err != nil {
		t.Fatalf("second Do() returned error: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 call after two Do() invocations, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// TestOnceWithRetry_ConcurrentDedup — 10 goroutines invoke Do() concurrently.
// The inner function executes exactly once and all callers get nil.
// ---------------------------------------------------------------------------

func TestOnceWithRetry_ConcurrentDedup(t *testing.T) {
	var o OnceWithRetry
	var calls atomic.Int32

	const numGoroutines = 10

	var start sync.WaitGroup
	start.Add(numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errs := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			start.Done()
			start.Wait() // Barrier: all goroutines enter Do() at ~same time
			errs <- o.Do(func() error {
				calls.Add(1)
				time.Sleep(10 * time.Millisecond) // ensure overlap
				return nil
			})
		}()
	}

	wg.Wait()
	close(errs)

	if n := calls.Load(); n != 1 {
		t.Errorf("expected exactly 1 call, got %d", n)
	}

	for err := range errs {
		if err != nil {
			t.Errorf("expected nil from Do(), got %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// TestOnceWithRetry_ErrorPropagation — Do() returns the function's error and
// leaves done=false, allowing retry.
// ---------------------------------------------------------------------------

func TestOnceWithRetry_ErrorPropagation(t *testing.T) {
	var o OnceWithRetry
	sentinel := errors.New("transient")

	err := o.Do(func() error {
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if o.done.Load() {
		t.Error("expected done to remain false after error")
	}
}

// ---------------------------------------------------------------------------
// TestOnceWithRetry_RetryAfterError — First Do() errors, second Do() succeeds.
// Both function invocations execute, and done is true after the second call.
// ---------------------------------------------------------------------------

func TestOnceWithRetry_RetryAfterError(t *testing.T) {
	var o OnceWithRetry
	var calls atomic.Int32
	sentinel := errors.New("first attempt failed")

	// First call: error
	err := o.Do(func() error {
		calls.Add(1)
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Errorf("first Do(): expected sentinel error, got %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("first Do(): expected 1 call, got %d", n)
	}
	if o.done.Load() {
		t.Error("expected done to be false after error")
	}

	// Second call: success
	err = o.Do(func() error {
		calls.Add(1)
		return nil
	})

	if err != nil {
		t.Fatalf("second Do(): expected nil, got %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("second Do(): expected 2 calls total, got %d", n)
	}
	if !o.done.Load() {
		t.Error("expected done to be true after successful retry")
	}
}

// ---------------------------------------------------------------------------
// TestOnceWithRetry_FastPath — After successful Do(), subsequent calls return
// nil via the lock-free atomic fast path without executing the function.
// ---------------------------------------------------------------------------

func TestOnceWithRetry_FastPath(t *testing.T) {
	var o OnceWithRetry

	// Prime the OnceWithRetry: successful execution
	err := o.Do(func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("first Do() returned error: %v", err)
	}

	// Second call: function would panic if executed — proves fast path
	err = o.Do(func() error {
		panic("should not be called")
	})
	if err != nil {
		t.Errorf("fast-path Do() returned error: %v", err)
	}
}
