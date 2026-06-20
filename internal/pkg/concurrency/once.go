// Package concurrency provides reusable synchronization primitives.
package concurrency

import (
	"sync"
	"sync/atomic"
)

// OnceWithRetry allows retrying a function on error while deduplicating
// concurrent executions. It provides a lock-free fast path for maximum
// read performance after successful execution.
//
// Unlike sync.Once, OnceWithRetry does not cache errors. If the function
// passed to Do returns an error, the error is propagated to the caller
// without marking the operation as complete. Subsequent calls to Do will
// re-execute the function.
type OnceWithRetry struct {
	done atomic.Bool
	mu   sync.Mutex
}

// Do calls the function f if and only if Do has not yet completed
// successfully. If f returns an error, Do returns that error and does
// not mark the operation as complete — subsequent calls will retry f.
//
// Once a call to f returns nil, all future calls to Do return nil
// immediately via a lock-free atomic load of the done flag.
func (o *OnceWithRetry) Do(f func() error) error {
	// Fast path: lock-free atomic check
	if o.done.Load() {
		return nil
	}

	// Slow path: exclusive lock and double-check
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.done.Load() {
		return nil
	}

	if err := f(); err != nil {
		return err // Propagate error without caching
	}

	// Memory barrier: guarantees visibility of external state mutations
	// prior to this store (Go memory model § happens-before)
	o.done.Store(true)
	return nil
}
