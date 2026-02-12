// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package concurrency

import (
	"context"
	"sync"
)

// WorkerPool manages a fixed number of workers to execute tasks concurrently.
type WorkerPool struct {
	maxWorkers int
	tasks      chan func(ctx context.Context)
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	closing    chan struct{}
	mu         sync.RWMutex
	closed     bool
	once       sync.Once
	submitWg   sync.WaitGroup
}

// NewWorkerPool creates and starts a new worker pool.
func NewWorkerPool(maxWorkers int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &WorkerPool{
		maxWorkers: maxWorkers,
		tasks:      make(chan func(ctx context.Context), maxWorkers*2),
		ctx:        ctx,
		cancel:     cancel,
		closing:    make(chan struct{}),
	}
	p.start()
	return p
}

func (p *WorkerPool) start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for task := range p.tasks {
				task(p.ctx)
			}
		}()
	}
}

// Submit adds a task to the pool. Returns true if the task was successfully queued.
func (p *WorkerPool) Submit(task func(ctx context.Context)) bool {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return false
	}
	p.submitWg.Add(1)
	p.mu.RUnlock()
	defer p.submitWg.Done()

	select {
	case p.tasks <- task:
		return true
	case <-p.closing:
		return false
	case <-p.ctx.Done():
		return false
	}
}

// Shutdown stops all workers and waits for them to finish.
func (p *WorkerPool) Shutdown() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.closing)
		p.mu.Unlock()

		p.submitWg.Wait()
		close(p.tasks)
		p.cancel()
		p.wg.Wait()
	})
}
