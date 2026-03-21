// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

type testEvent struct {
	typeName string
	val      interface{}
}

func (e testEvent) Type() string {
	if e.typeName != "" {
		return e.typeName
	}
	return "testEvent"
}

type errSubscriber struct {
	err error
}

func (s *errSubscriber) Handle(ctx context.Context, e events.Event) error {
	return s.err
}

type panicSubscriber struct {
	msg string
}

func (s *panicSubscriber) Handle(ctx context.Context, e events.Event) error {
	panic(s.msg)
}

func TestSimpleEventBus_PublishSubscribe(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	received := make(chan events.Event, 1)

	bus.Subscribe(func(e events.Event) {
		received <- e
	})

	ev := testEvent{val: "hello"}
	err := bus.Publish(context.Background(), ev)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.(testEvent).val != "hello" {
			t.Errorf("expected hello, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSimpleEventBus_ErrorAggregation(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())

	err1 := errors.New("err 1")
	err2 := errors.New("err 2")

	bus.SubscribeSubscriber("testEvent", &errSubscriber{err: err1})
	bus.SubscribeSubscriber("testEvent", &errSubscriber{err: err2})
	bus.SubscribeSubscriber("testEvent", &panicSubscriber{msg: "panic 1"})

	err := bus.Publish(context.Background(), testEvent{})
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "err 1") || !strings.Contains(errMsg, "err 2") || !strings.Contains(errMsg, "subscriber panicked: panic 1") {
		t.Errorf("error aggregation failed, got: %v", errMsg)
	}

	// Verify it contains multiple joined errors (Go 1.20+)
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Error("expected a joined error")
	} else if len(joined.Unwrap()) != 3 {
		t.Errorf("expected 3 joined errors, got %d", len(joined.Unwrap()))
	}
}

func TestSimpleEventBus_PanicRecovery(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())

	bus.Subscribe(func(e events.Event) {
		panic("boom")
	})

	bus.Subscribe(func(e events.Event) {
		// Normal subscriber
	})

	err := bus.Publish(context.Background(), testEvent{})
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}

	if !strings.Contains(err.Error(), "subscriber panicked: boom") {
		t.Errorf("expected panic message in error, got %v", err)
	}
}

func TestSimpleEventBus_FlushAndShutdown(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())

	if err := bus.Flush(context.Background()); err != nil {
		t.Errorf("Flush failed: %v", err)
	}

	if err := bus.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	if err := bus.Publish(context.Background(), testEvent{}); !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestSimpleEventBus_ContextCancellation(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bus.Publish(ctx, testEvent{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

type unknownEvent struct{}

func (e unknownEvent) Type() string { return "UnknownEvent" }

func TestGlobalSubscriber_NewEventType(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	received := make(chan events.Event, 1)

	bus.Subscribe(func(e events.Event) {
		received <- e
	})

	// This event type was NOT in the previous hardcoded allKnownTypes list
	ev := unknownEvent{}
	err := bus.Publish(context.Background(), ev)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.Type() != "UnknownEvent" {
			t.Errorf("expected UnknownEvent, got %s", got.Type())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Global subscriber did NOT receive UnknownEvent")
	}
}

func TestSimpleEventBus_Race(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine publishing events
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = bus.Publish(ctx, events.StatusUpdate{Message: "test"})
			}
		}
	}()

	// Goroutine subscribing to events
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			bus.Subscribe(func(e events.Event) {})
			time.Sleep(time.Microsecond)
		}
	}()

	// Wait a bit to let them race
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

type deadlockSubscriber struct {
	bus events.EventBus
}

func (s *deadlockSubscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Type() == "StatusUpdate" {
		// Publish a DIFFERENT event type to avoid infinite recursion
		return s.bus.Publish(ctx, events.TurnStarted{})
	}
	return nil
}

func TestSimpleEventBus_Deadlock(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	ctx := context.Background()

	sub := &deadlockSubscriber{bus: bus}
	bus.SubscribeSubscriber("StatusUpdate", sub)

	// This goroutine will try to get a Write lock while the Publish (RLock) is active
	// and waiting for the recursive Publish (RLock) which will be blocked by this writer.
	go func() {
		for i := 0; i < 100; i++ {
			bus.Subscribe(func(e events.Event) {})
			time.Sleep(time.Microsecond)
		}
	}()

	done := make(chan error, 1)
	go func() {
		// Start publishing. It will hit the deadlockSubscriber.
		done <- bus.Publish(ctx, events.StatusUpdate{Message: "test"})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("Publish returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Deadlock detected in Publish")
	}
}
