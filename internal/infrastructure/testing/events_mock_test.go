// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"reflect"
	"testing"
)

func TestTestEventBus(t *testing.T) {
	bus := &TestEventBus{}

	type MyEvent struct{ ID int }
	type OtherEvent struct{}

	_ = bus.Publish(MyEvent{ID: 1})
	_ = bus.Publish(OtherEvent{})

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
