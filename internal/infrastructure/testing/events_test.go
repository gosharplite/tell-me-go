// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"context"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type myEvent struct{ ID int }
type otherEvent struct{}

func (e myEvent) Type() string    { return "myEvent" }
func (e otherEvent) Type() string { return "otherEvent" }

func TestTestEventBus(t *testing.T) {
	t.Parallel()
	bus := &TestEventBus{}
	ctx := context.Background()

	require.NoError(t, bus.Publish(ctx, myEvent{ID: 1}))
	require.NoError(t, bus.Publish(ctx, otherEvent{}))

	if len(bus.getEvents()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(bus.getEvents()))
	}

	if !bus.AssertEventPublished(reflect.TypeOf(myEvent{})) {
		t.Error("myEvent should be published")
	}

	if !bus.AssertEventPublished(reflect.TypeOf(otherEvent{})) {
		t.Error("otherEvent should be published")
	}

	filtered := bus.FilterEvents(reflect.TypeOf(myEvent{}))
	if len(filtered) != 1 || filtered[0].(myEvent).ID != 1 {
		t.Errorf("FilterEvents failed: %v", filtered)
	}

	bus.clear()
	if len(bus.getEvents()) != 0 {
		t.Error("Clear failed")
	}
}

func TestTestEventBus_Subscribe(t *testing.T) {
	t.Parallel()
	bus := &TestEventBus{}

	var receivedID int
	bus.Subscribe(func(e events.Event) {
		if ev, ok := e.(myEvent); ok {
			receivedID = ev.ID
		}
	})

	err := bus.Publish(context.Background(), myEvent{ID: 42})
	require.NoError(t, err)
	assert.Equal(t, 42, receivedID, "Subscriber should have received the event with correct ID")
}

func TestTestEventBus_NoOps(t *testing.T) {
	t.Parallel()
	bus := &TestEventBus{}
	ctx := context.Background()

	assert.NoError(t, bus.Flush(ctx))
	assert.NoError(t, bus.Shutdown(ctx))
}

func TestCountingEventBus(t *testing.T) {
	t.Parallel()
	bus := NewCountingEventBus()
	ctx := context.Background()

	require.NoError(t, bus.Publish(ctx, myEvent{}))
	require.NoError(t, bus.Publish(ctx, myEvent{}))

	assert.Equal(t, 2, bus.GetCount())
}
