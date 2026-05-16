// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package eventstest_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type myEvent struct{ ID int }
type otherEvent struct{}

func (e myEvent) Type() string    { return "myEvent" }
func (e otherEvent) Type() string { return "otherEvent" }

func TestTestEventBus(t *testing.T) {
	t.Parallel()
	bus := &eventstest.TestEventBus{}
	ctx := context.Background()

	require.NoError(t, bus.Publish(ctx, myEvent{ID: 1}))
	require.NoError(t, bus.Publish(ctx, otherEvent{}))

	if len(bus.GetEvents()) != 2 {
		t.Errorf("Expected 2 events, got %d", len(bus.GetEvents()))
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

	bus.Clear()
	if len(bus.GetEvents()) != 0 {
		t.Error("Clear failed")
	}
}

func TestTestEventBus_Subscribe(t *testing.T) {
	t.Parallel()
	bus := &eventstest.TestEventBus{}

	var receivedID int
	bus.Subscribe(func(ctx context.Context, e events.Event) {
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
	bus := &eventstest.TestEventBus{}
	ctx := context.Background()

	assert.NoError(t, bus.Flush(ctx))
	assert.NoError(t, bus.Shutdown(ctx))
}

// ----- Sub-Task 5A: Error injection setters and error return paths -----

func TestTestEventBus_ErrorInjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("SetPublishErr", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("pub down"))

		err := bus.Publish(ctx, myEvent{ID: 1})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pub down")
		assert.Empty(t, bus.GetEvents(), "event must not be recorded when publish error is set")

		// Reset and verify normal behavior resumes
		bus.SetPublishErr(nil)
		assert.NoError(t, bus.Publish(ctx, myEvent{ID: 2}))
		assert.Len(t, bus.GetEvents(), 1)
	})

	t.Run("SetFlushErr", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetFlushErr(errors.New("flush fail"))
		assert.EqualError(t, bus.Flush(ctx), "flush fail")

		bus.SetFlushErr(nil)
		assert.NoError(t, bus.Flush(ctx))
	})

	t.Run("SetShutdownErr", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetShutdownErr(errors.New("shutdown hang"))
		assert.EqualError(t, bus.Shutdown(ctx), "shutdown hang")

		bus.SetShutdownErr(nil)
		assert.NoError(t, bus.Shutdown(ctx))
	})
}

// ----- Sub-Task 5B: Publish context-cancelled branch -----

func TestTestEventBus_Publish_ContextCancelled(t *testing.T) {
	t.Parallel()

	bus := &eventstest.TestEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bus.Publish(ctx, myEvent{ID: 1})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, bus.GetEvents(), "event must not be recorded when context is cancelled")
}

// ----- Sub-Task 5C: Listen and WaitStarted -----

func TestTestEventBus_Listen(t *testing.T) {
	t.Parallel()

	bus := &eventstest.TestEventBus{}
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- bus.Listen(ctx)
	}()

	// Verify Listen blocks and does not return before cancellation
	select {
	case <-errCh:
		t.Fatal("Listen returned before context was cancelled")
	case <-time.After(50 * time.Millisecond):
		// expected — still blocking
	}

	cancel()

	// Listen should return ctx.Err() after cancellation
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(1 * time.Second):
		t.Fatal("Listen did not return after context cancellation")
	}
}

func TestTestEventBus_WaitStarted(t *testing.T) {
	bus := &eventstest.TestEventBus{}
	bus.WaitStarted() // must not panic or block
}

// ----- Sub-Task 5D: AssertEventPublished edge cases -----

func TestTestEventBus_AssertEventPublished_EdgeCases(t *testing.T) {
	t.Parallel()
	bus := &eventstest.TestEventBus{}

	require.NoError(t, bus.Publish(context.Background(), myEvent{ID: 1}))

	// Nil type — should return false, no panic
	assert.False(t, bus.AssertEventPublished(nil))

	// Concrete type match
	assert.True(t, bus.AssertEventPublished(reflect.TypeOf(myEvent{})))

	// Wrong concrete type
	assert.False(t, bus.AssertEventPublished(reflect.TypeOf(otherEvent{})))
}

// ----- CleanupBus tests -----

// spyShutdownBus wraps TestEventBus and signals a channel when Shutdown is called.
type spyShutdownBus struct {
	*eventstest.TestEventBus
	shutdownCalled chan struct{}
}

func (s *spyShutdownBus) Shutdown(ctx context.Context) error {
	err := s.TestEventBus.Shutdown(ctx)
	s.shutdownCalled <- struct{}{}
	return err
}

func TestCleanupBus(t *testing.T) {
	t.Run("shutdown_called_on_cleanup", func(t *testing.T) {
		var spy *spyShutdownBus
		t.Run("inner", func(t *testing.T) {
			spy = &spyShutdownBus{
				TestEventBus:   &eventstest.TestEventBus{},
				shutdownCalled: make(chan struct{}, 1),
			}
			eventstest.CleanupBus(t, spy)
		})
		// t.Cleanup fires after the inner subtest returns but before
		// t.Run returns. At this point Shutdown must have been called.
		if spy == nil {
			t.Fatal("spy was not initialised")
		}
		select {
		case <-spy.shutdownCalled:
			// pass — Shutdown was invoked by the cleanup hook
		case <-time.After(time.Second):
			t.Error("expected Shutdown to be called on cleanup, but it was not")
		}
	})

	t.Run("shutdown_error_is_logged", func(t *testing.T) {
		bus := &eventstest.TestEventBus{}
		bus.SetShutdownErr(errors.New("boom"))
		eventstest.CleanupBus(t, bus)
		// The error will be logged via t.Logf when cleanup fires.
		// This subtest simply asserts no panic occurs.
	})
}
