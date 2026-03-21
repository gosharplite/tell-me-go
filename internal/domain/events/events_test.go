// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"context"
	"errors"
	"strings"
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
