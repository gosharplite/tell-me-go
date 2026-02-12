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
		p := NewWorkerPool(2)
		p.Shutdown()
		p.Shutdown() // Should not panic or hang
	})

	t.Run("SubmitAfterShutdown", func(t *testing.T) {
		p := NewWorkerPool(2)
		p.Shutdown()

		ok := p.Submit(func(ctx context.Context) {})
		if ok {
			t.Error("Submit should return false after Shutdown")
		}
	})

	t.Run("WaitTasksOnShutdown", func(t *testing.T) {
		p := NewWorkerPool(1)
		start := make(chan struct{})
		done := make(chan struct{})

		p.Submit(func(ctx context.Context) {
			close(start)
			time.Sleep(100 * time.Millisecond)
			close(done)
		})

		<-start
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
		p.Submit(func(ctx context.Context) {
			defer wg.Done()
			counter <- 1
		})
	}

	wg.Wait()
	if len(counter) != numTasks {
		t.Errorf("Expected %d tasks to complete, got %d", numTasks, len(counter))
	}
}

func TestWorkerPool_SubmitBlocking(t *testing.T) {
	t.Parallel()

	p := NewWorkerPool(1)
	// Task 1: starts running and blocks
	p.Submit(func(ctx context.Context) {
		time.Sleep(200 * time.Millisecond)
	})
	// Task 2 & 3: fill the channel (size is 1*2 = 2)
	p.Submit(func(ctx context.Context) {})
	p.Submit(func(ctx context.Context) {})

	// Task 4: Should block in Submit's select
	submitResult := make(chan bool)
	go func() {
		submitResult <- p.Submit(func(ctx context.Context) {})
	}()

	// Ensure the goroutine is likely blocked in select
	time.Sleep(50 * time.Millisecond)

	p.Shutdown()

	select {
	case ok := <-submitResult:
		if ok {
			t.Error("Submit should return false when pool is shut down while blocked")
		}
	case <-time.After(1 * time.Second):
		t.Error("Submit did not unblock after Shutdown")
	}
}
