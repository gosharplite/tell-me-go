// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"sync"
)

// Task represents a unit of work for the worker pool.
type Task func(ctx context.Context)

// WorkerPool manages a fixed number of workers to execute tasks concurrently.
type WorkerPool struct {
	maxWorkers int
	tasks      chan Task
	wg         sync.WaitGroup
	quit       chan struct{}
	once       sync.Once
}

// NewWorkerPool creates and starts a new worker pool.
func NewWorkerPool(maxWorkers int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	p := &WorkerPool{
		maxWorkers: maxWorkers,
		tasks:      make(chan Task, maxWorkers*2),
		quit:       make(chan struct{}),
	}
	p.start()
	return p
}

func (p *WorkerPool) start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for {
				select {
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					// We use a background context here because the task itself
					// should handle its own context if passed.
					task(context.Background())
				case <-p.quit:
					return
				}
			}
		}()
	}
}

// Submit adds a task to the pool.
func (p *WorkerPool) Submit(task Task) {
	select {
	case p.tasks <- task:
	case <-p.quit:
	}
}

// Shutdown stops all workers and waits for them to finish.
func (p *WorkerPool) Shutdown() {
	p.once.Do(func() {
		close(p.quit)
		close(p.tasks)
		p.wg.Wait()
	})
}
