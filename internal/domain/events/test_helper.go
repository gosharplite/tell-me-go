// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"reflect"
	"sync"
)

// TestEventBus is a thread-safe event bus designed for unit tests.
// It records all published events for assertion.
type TestEventBus struct {
	mu     sync.RWMutex
	events []Event
	subs   []func(Event)
}

// Publish records the event and notifies subscribers.
func (b *TestEventBus) Publish(e Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	subs := make([]func(Event), len(b.subs))
	copy(subs, b.subs)
	b.mu.Unlock()

	for _, sub := range subs {
		sub(e)
	}
}

// Subscribe adds a new listener.
func (b *TestEventBus) Subscribe(sub func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, sub)
}

// GetEvents returns a copy of all recorded events.
func (b *TestEventBus) GetEvents() []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	res := make([]Event, len(b.events))
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
func (b *TestEventBus) FilterEvents(t reflect.Type) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var res []Event
	for _, e := range b.events {
		if reflect.TypeOf(e) == t {
			res = append(res, e)
		}
	}
	return res
}

// CountingEventBus records the number of events published and allows waiting for a target count.
type CountingEventBus struct {
	SimpleEventBus
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
func (b *CountingEventBus) Publish(e Event) {
	b.SimpleEventBus.Publish(e)
	b.mu.Lock()
	b.count++
	b.cond.Broadcast()
	b.mu.Unlock()
}

// WaitCount blocks until at least target events have been published.
func (b *CountingEventBus) WaitCount(target int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.count < target {
		b.cond.Wait()
	}
}

// GetCount returns the current number of published events.
func (b *CountingEventBus) GetCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}
