// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
)

// Event represents a generic signal from the SessionManager.
type Event interface {
	Type() string
}

// Subscriber defines the interface for event handlers.
// CRITICAL: Implementations MUST monitor ctx.Done() and abort
// long-running operations immediately to prevent goroutine leaks.
type Subscriber interface {
	Handle(ctx context.Context, e Event) error
}

var (
	ErrBusClosed         = errors.New("event bus is closed")
	ErrBusNotInitialized = errors.New("event bus is nil or uninitialized")
	ErrAlreadyListening  = errors.New("event bus is already listening")
)

const (
	defaultQueueSize         = 1024
	defaultAsyncDispatch     = true
	defaultMaxConcurrentSubs = 1024
)

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(sub func(context.Context, Event))
	Shutdown(ctx context.Context) error
	Flush(ctx context.Context) error
	// Listen starts the event bus's internal workers and blocks until the context is canceled.
	// [ARCHITECTURAL REFACTOR] This replaces the previous fire-and-forget goroutine pattern.
	Listen(ctx context.Context) error
	// WaitStarted blocks until the listener goroutine is fully initialized.
	WaitStarted()
}

type subscriberWrapper struct {
	sub Subscriber
	ch  chan Event
}

type funcSubscriber struct {
	f func(context.Context, Event)
}

func (s *funcSubscriber) Handle(ctx context.Context, e Event) error {
	s.f(ctx, e)
	return nil
}
