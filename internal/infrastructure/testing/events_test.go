// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"context"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
)

func TestTestEventBus(t *testing.T) {
	bus := &TestEventBus{}

	type MyEvent struct{ ID int }
	type OtherEvent struct{}

	bus.Publish(MyEvent{ID: 1})
	bus.Publish(OtherEvent{})

	if len(bus.getEvents()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(bus.getEvents()))
	}

	if !bus.AssertEventPublished(reflect.TypeOf(MyEvent{})) {
		t.Error("MyEvent should be published")
	}

	if !bus.AssertEventPublished(reflect.TypeOf(OtherEvent{})) {
		t.Error("OtherEvent should be published")
	}

	filtered := bus.FilterEvents(reflect.TypeOf(MyEvent{}))
	if len(filtered) != 1 || filtered[0].(MyEvent).ID != 1 {
		t.Errorf("FilterEvents failed: %v", filtered)
	}

	bus.clear()
	if len(bus.getEvents()) != 0 {
		t.Error("Clear failed")
	}
}

func TestTestEventBus_Subscribe(t *testing.T) {
	bus := &TestEventBus{}
	type MyEvent struct{ ID int }

	var receivedID int
	bus.Subscribe(func(e events.Event) {
		if ev, ok := e.(MyEvent); ok {
			receivedID = ev.ID
		}
	})

	bus.Publish(MyEvent{ID: 42})
	assert.Equal(t, 42, receivedID, "Subscriber should have received the event with correct ID")
}

func TestTestEventBus_NoOps(t *testing.T) {
	bus := &TestEventBus{}
	ctx := context.Background()

	assert.NoError(t, bus.Flush(ctx))
	assert.NoError(t, bus.Shutdown(ctx))
}

func TestCountingEventBus(t *testing.T) {
	bus := NewCountingEventBus()
	type MyEvent struct{}

	bus.Publish(MyEvent{})
	bus.Publish(MyEvent{})

	assert.Equal(t, 2, bus.GetCount())
}
