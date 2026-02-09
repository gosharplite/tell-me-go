// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"reflect"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// TestEventBus is a thread-safe event bus designed for unit tests.
// It records all published events for assertion.
type TestEventBus struct {
	mu     sync.RWMutex
	events []events.Event
	subs   []func(events.Event)
}

// Publish records the event and notifies subscribers.
func (b *TestEventBus) Publish(e events.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	subs := make([]func(events.Event), len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, sub := range subs {
		sub(e)
	}
}

// Subscribe adds a new listener.
func (b *TestEventBus) Subscribe(sub func(events.Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, sub)
}

// GetEvents returns a copy of all recorded events.
func (b *TestEventBus) GetEvents() []events.Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	res := make([]events.Event, len(b.events))
	copy(res, b.events)
	return res
}

// Clear removes all recorded events.
func (b *TestEventBus) Clear() {
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
type CountingEventBus struct {
	events.SimpleEventBus
	mu    sync.RWMutex
	count int
	cond  *sync.Cond
}

// NewCountingEventBus creates a new CountingEventBus.
func NewCountingEventBus() *CountingEventBus {
	cb := &CountingEventBus{}
	cb.cond = sync.NewCond(&cb.mu)
	return cb
}

// Publish notifies subscribers and increments the internal counter.
func (b *CountingEventBus) Publish(e events.Event) {
	b.SimpleEventBus.Publish(e)
	b.mu.Lock()
	b.count++
	b.cond.Broadcast()
	b.mu.Unlock()
}

// GetCount returns the current number of published events.
func (b *CountingEventBus) GetCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}
