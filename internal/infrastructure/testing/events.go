// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"context"
	"reflect"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// TestEventBus is a thread-safe event bus designed for unit tests.
// It records all published events for assertion and allows simulating errors.
type TestEventBus struct {
	mu          sync.RWMutex
	events      []events.Event
	subs        []func(context.Context, events.Event)
	publishErr  error
	flushErr    error
	shutdownErr error
}

// SetPublishErr sets the error to be returned by Publish.
func (b *TestEventBus) SetPublishErr(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.publishErr = err
}

// SetFlushErr sets the error to be returned by Flush.
func (b *TestEventBus) SetFlushErr(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushErr = err
}

// SetShutdownErr sets the error to be returned by Shutdown.
func (b *TestEventBus) SetShutdownErr(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.shutdownErr = err
}

// Publish records the event and notifies subscribers. Returns publishErr if set.
func (b *TestEventBus) Publish(ctx context.Context, e events.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.RLock()
	pubErr := b.publishErr
	b.mu.RUnlock()
	if pubErr != nil {
		return pubErr
	}

	b.mu.Lock()
	b.events = append(b.events, e)
	subs := make([]func(context.Context, events.Event), len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, sub := range subs {
		sub(ctx, e)
	}
	return nil
}

// Subscribe adds a new listener.
func (b *TestEventBus) Subscribe(sub func(context.Context, events.Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, sub)
}

// Shutdown returns shutdownErr if set.
func (b *TestEventBus) Shutdown(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.shutdownErr
}

// Flush returns flushErr if set.
func (b *TestEventBus) Flush(ctx context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.flushErr
}

// GetEvents returns a copy of all recorded events.
func (b *TestEventBus) getEvents() []events.Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	res := make([]events.Event, len(b.events))
	copy(res, b.events)
	return res
}

// Clear removes all recorded events.
func (b *TestEventBus) clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = nil
}

// AssertEventPublished checks if an event of a specific type was published.
func (b *TestEventBus) AssertEventPublished(t reflect.Type) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, e := range b.events {
		if reflect.TypeOf(e) == t {
			return true
		}
	}
	return false
}

// FilterEvents returns only events of the specified type.
func (b *TestEventBus) FilterEvents(t reflect.Type) []events.Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var res []events.Event
	for _, e := range b.events {
		if reflect.TypeOf(e) == t {
			res = append(res, e)
		}
	}
	return res
}

// CountingEventBus records the number of events published and allows waiting for a target count.
type countingEventBus struct {
	*events.SimpleEventBus
	mu    sync.RWMutex
	count int
	cond  *sync.Cond
}

// NewCountingEventBus creates a new CountingEventBus.
func NewCountingEventBus() *countingEventBus {
	cb := &countingEventBus{
		SimpleEventBus: events.NewSimpleEventBus(context.Background(), events.WithWorkers(0)),
	}
	cb.cond = sync.NewCond(&cb.mu)
	return cb
}

// Publish notifies subscribers and increments the internal counter.
func (b *countingEventBus) Publish(ctx context.Context, e events.Event) error {
	err := b.SimpleEventBus.Publish(ctx, e)
	b.mu.Lock()
	b.count++
	b.cond.Broadcast()
	b.mu.Unlock()
	return err
}

// GetCount returns the current number of published events.
func (b *countingEventBus) GetCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}
