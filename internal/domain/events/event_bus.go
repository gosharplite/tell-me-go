// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"log/slog"
	"sync"
)

// SimpleEventBus is an implementation of EventBus.
type SimpleEventBus struct {
	mu                sync.RWMutex
	subscribers       map[string][]*subscriberWrapper
	globalSubscribers []*subscriberWrapper
	closed            bool
	closing           chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	log               *slog.Logger

	running   bool
	started   chan struct{}
	startOnce sync.Once

	queueSize     int
	asyncDispatch bool           // If false, runs synchronously
	workerWG      sync.WaitGroup // Tracks active worker goroutines for subscribers
	wgWait        func(*sync.WaitGroup)
	pendingMu     sync.Mutex
	cond          *sync.Cond
	pendingCount  int
}

// busOption defines a functional option for configuring the SimpleEventBus.
type busOption func(*SimpleEventBus)

// WithLogger sets the logger for the SimpleEventBus.
func WithLogger(l *slog.Logger) busOption {
	return func(b *SimpleEventBus) {
		b.log = l
	}
}

// WithQueueSize sets the size of the per-subscriber event queue.
func WithQueueSize(size int) busOption {
	return func(b *SimpleEventBus) {
		b.queueSize = size
	}
}

// WithAsync sets whether the event bus runs asynchronously.
func WithAsync(async bool) busOption {
	return func(b *SimpleEventBus) {
		b.asyncDispatch = async
	}
}

// WithMaxConcurrentSubscribers is deprecated and kept for backward compatibility.
func WithMaxConcurrentSubscribers(n int) busOption {
	return func(b *SimpleEventBus) {}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus(ctx context.Context, opts ...busOption) *SimpleEventBus {
	ctx, cancel := context.WithCancel(ctx)
	b := &SimpleEventBus{
		subscribers:       make(map[string][]*subscriberWrapper),
		globalSubscribers: make([]*subscriberWrapper, 0),
		closing:           make(chan struct{}),
		ctx:               ctx,
		cancel:            cancel,
		log:               slog.Default(),
		asyncDispatch:     defaultAsyncDispatch,
		queueSize:         defaultQueueSize,
		started:           make(chan struct{}),
		wgWait:            (*sync.WaitGroup).Wait,
	}
	b.cond = sync.NewCond(&b.pendingMu)

	for _, opt := range opts {
		opt(b)
	}

	return b
}

func (b *SimpleEventBus) getLogger() *slog.Logger {
	if b == nil || b.log == nil {
		return slog.Default()
	}
	return b.log
}
