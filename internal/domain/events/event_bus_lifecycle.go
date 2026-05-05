// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"log/slog"
	"runtime/debug"

	"golang.org/x/sync/errgroup"
)

func (b *SimpleEventBus) WaitStarted() {
	if b == nil {
		return
	}
	<-b.started
}

func (b *SimpleEventBus) signalStarted() {
	if b == nil {
		return
	}
	b.startOnce.Do(func() {
		close(b.started)
	})
}

func (b *SimpleEventBus) incPending() {
	b.pendingMu.Lock()
	b.pendingCount++
	b.pendingMu.Unlock()
}

func (b *SimpleEventBus) decPending() {
	b.pendingMu.Lock()
	b.pendingCount--
	if b.pendingCount == 0 {
		b.cond.Broadcast()
	}
	b.pendingMu.Unlock()
}

// Shutdown gracefully stops the event bus.
func (b *SimpleEventBus) Shutdown(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	if b.cancel != nil {
		b.cancel()
	}

	// Wait for active workers to finish
	done := make(chan struct{})
	go b.waitWorkers(done)

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitWorkers blocks until all worker goroutines have drained, then closes done.
// Uses the bus's wait func so tests can inject a panic.
func (b *SimpleEventBus) waitWorkers(done chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			if b.log != nil {
				b.log.Error("panic in event bus shutdown wait", "error", r, "stack", string(debug.Stack()))
			}
			close(done)
		}
	}()
	b.wgWait(&b.workerWG)
	close(done)
}

// Flush waits for all currently queued events to be processed or context timeout.
func (b *SimpleEventBus) Flush(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return ErrBusClosed
	}

	done := make(chan struct{})
	var cancelled bool

	go b.flushWaiter(done, &cancelled)

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return b.cancelFlushWaiter(&cancelled, ctx.Err())
	case <-b.ctx.Done():
		return b.cancelFlushWaiter(&cancelled, ErrBusClosed)
	}
}

// flushWaiter blocks on b.cond until pendingCount drops to 0 or the cancelled
// flag is set by cancelFlushWaiter. It always closes done when it returns.
// Recovers from any panic in cond.Wait to avoid leaving Flush blocked forever.
func (b *SimpleEventBus) flushWaiter(done chan<- struct{}, cancelled *bool) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			if b.log != nil {
				b.log.Error("panic in event bus flush wait", "error", r, "stack", string(debug.Stack()))
			}
		}
	}()

	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	for b.pendingCount > 0 && !*cancelled {
		b.cond.Wait()
	}
}

// cancelFlushWaiter signals the flushWaiter goroutine to stop waiting and
// returns the supplied error. Used by both ctx-cancellation and bus-shutdown
// branches of Flush.
func (b *SimpleEventBus) cancelFlushWaiter(cancelled *bool, err error) error {
	b.pendingMu.Lock()
	*cancelled = true
	b.cond.Broadcast()
	b.pendingMu.Unlock()
	return err
}

// Listen starts all per-subscriber background worker loops and blocks until the context is canceled.
// Implementation follows the coordinated concurrency pattern using errgroup.
func (b *SimpleEventBus) Listen(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	b.mu.RLock()
	async := b.asyncDispatch
	b.mu.RUnlock()

	if !async {
		b.signalStarted()
		return nil
	}

	// Create a derived context for the listener
	g, listenCtx := errgroup.WithContext(ctx)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.signalStarted()
		return ErrBusClosed
	}

	if b.running {
		b.mu.Unlock()
		return nil // Already running
	}

	b.running = true
	b.listenCtx = listenCtx
	b.listenG = g

	// Coordinated shutdown: even if there are no subscribers,
	// the bus should stay "running" until the context is cancelled.
	b.listenG.Go(func() error {
		<-listenCtx.Done()
		return nil
	})

	// Collect all current subscribers to start their workers
	var wrappers []*subscriberWrapper
	for _, ws := range b.subscribers {
		wrappers = append(wrappers, ws...)
	}
	wrappers = append(wrappers, b.globalSubscribers...)

	for _, w := range wrappers {
		w := w
		b.workerWG.Add(1)
		b.listenG.Go(func() error {
			defer func() {
				if r := recover(); r != nil {
					b.getLogger().Error("panic in event bus subscriber loop",
						slog.Any("error", r),
						slog.String("stack", string(debug.Stack())))
				}
			}()
			return b.subscriberLoop(b.listenCtx, w)
		})
	}

	// Signal that the listener is fully initialized
	b.signalStarted()
	b.mu.Unlock()

	err := b.listenG.Wait()

	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	return err
}
