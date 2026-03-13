// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package concurrency

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWorkerPool_BoundaryConditions(t *testing.T) {
	t.Parallel()

	t.Run("ZeroWorkers", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(0)
		defer p.Shutdown()

		if p.maxWorkers != 1 {
			t.Errorf("Expected maxWorkers to be 1 when initialized with 0, got %d", p.maxWorkers)
		}

		done := make(chan struct{})
		ok := p.Submit(func(ctx context.Context) {
			close(done)
		})

		if !ok {
			t.Error("Submit should return true for zero-worker pool (defaulted to 1)")
		}

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Error("Task did not execute in zero-worker pool")
		}
	})

	t.Run("NegativeWorkers", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(-5)
		defer p.Shutdown()

		if p.maxWorkers != 1 {
			t.Errorf("Expected maxWorkers to be 1 when initialized with negative value, got %d", p.maxWorkers)
		}

		done := make(chan struct{})
		ok := p.Submit(func(ctx context.Context) {
			close(done)
		})

		if !ok {
			t.Error("Submit should return true for negative-worker pool (defaulted to 1)")
		}

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Error("Task did not execute in negative-worker pool")
		}
	})
}

func TestWorkerPool_ShutdownBehavior(t *testing.T) {
	t.Parallel()

	t.Run("IdempotentShutdown", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(2)
		p.Shutdown()
		p.Shutdown() // Should not panic or hang
	})

	t.Run("SubmitAfterShutdown", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(2)
		p.Shutdown()

		ok := p.Submit(func(ctx context.Context) {})
		if ok {
			t.Error("Submit should return false after Shutdown")
		}
	})

	t.Run("WaitTasksOnShutdown", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(1)
		start := make(chan struct{})
		block := make(chan struct{})
		done := make(chan struct{})

		p.Submit(func(ctx context.Context) {
			close(start)
			select {
			case <-block:
			case <-ctx.Done():
			}
			close(done)
		})

		<-start
		close(block)
		p.Shutdown()

		select {
		case <-done:
			// Success: Shutdown waited for task
		default:
			t.Error("Shutdown did not wait for running task")
		}
	})
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("TaskRespectsPoolContext", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(1)
		taskStarted := make(chan struct{})
		taskDone := make(chan struct{})

		p.Submit(func(ctx context.Context) {
			close(taskStarted)
			<-ctx.Done()
			close(taskDone)
		})

		<-taskStarted
		p.Shutdown() // This cancels p.ctx

		select {
		case <-taskDone:
			// Success
		case <-time.After(1 * time.Second):
			t.Error("Task did not respect pool context cancellation")
		}
	})

	t.Run("SubmitAfterCancel", func(t *testing.T) {
		t.Parallel()
		p := NewWorkerPool(1)
		// We can't directly cancel p.ctx since it's private, but Shutdown calls cancel().
		p.Shutdown()

		ok := p.Submit(func(ctx context.Context) {})
		if ok {
			t.Error("Submit should return false after pool context is cancelled via Shutdown")
		}
	})
}

func TestWorkerPool_Concurrency(t *testing.T) {
	t.Parallel()

	numWorkers := 5
	numTasks := 20
	p := NewWorkerPool(numWorkers)
	defer p.Shutdown()

	var wg sync.WaitGroup
	wg.Add(numTasks)
	counter := make(chan int, numTasks)

	for i := 0; i < numTasks; i++ {
		for !p.Submit(func(ctx context.Context) {
			defer wg.Done()
			counter <- 1
		}) {
			time.Sleep(10 * time.Millisecond)
		}
	}

	wg.Wait()
	if len(counter) != numTasks {
		t.Errorf("Expected %d tasks to complete, got %d", numTasks, len(counter))
	}
}

func TestWorkerPool_SubmitFailFast(t *testing.T) {
	t.Parallel()

	p := NewWorkerPool(1)

	// Ensure the first task starts running and blocks
	startCh := make(chan struct{})
	blockCh := make(chan struct{}) // 1. Create the blocking channel

	p.Submit(func(ctx context.Context) {
		close(startCh)

		// 2. Safely block until test finishes or context cancels
		select {
		case <-blockCh:
		case <-ctx.Done():
		}
	})
	<-startCh

	// Task 2 & 3: fill the channel buffer (size is 1*2 = 2)
	ok2 := p.Submit(func(ctx context.Context) {})
	ok3 := p.Submit(func(ctx context.Context) {})

	if !ok2 || !ok3 {
		t.Errorf("Expected tasks 2 and 3 to be submitted successfully, got %v, %v", ok2, ok3)
	}

	// Task 4: Should fail fast
	ok4 := p.Submit(func(ctx context.Context) {})
	if ok4 {
		t.Error("Expected task 4 to fail fast and return false")
	}

	// 3. Unblock the worker at the very end of the test so it can exit cleanly
	close(blockCh)
	p.Shutdown()
}
