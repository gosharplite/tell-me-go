// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
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

type funcSubscriberWithErr struct {
	f func(context.Context, events.Event) error
}

func (s *funcSubscriberWithErr) Handle(ctx context.Context, e events.Event) error {
	return s.f(ctx, e)
}

func TestSimpleEventBus_PublishSubscribe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx)
	received := make(chan events.Event, 1)

	bus.Subscribe(func(ctx context.Context, e events.Event) {
		received <- e
	})

	ev := testEvent{val: "hello"}
	err := bus.Publish(ctx, ev)
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
	t.Parallel()

	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger), events.WithWorkers(0))

	err1 := errors.New("err 1")
	err2 := errors.New("err 2")

	bus.SubscribeSubscriber("testEvent", &errSubscriber{err: err1})
	bus.SubscribeSubscriber("testEvent", &errSubscriber{err: err2})
	bus.SubscribeSubscriber("testEvent", &panicSubscriber{msg: "panic 1"})

	err := bus.Publish(context.Background(), testEvent{typeName: "testEvent"})
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "err 1") || !strings.Contains(errMsg, "err 2") || !strings.Contains(errMsg, "subscriber panicked: panic 1") {
		t.Errorf("error aggregation failed, got: %v", errMsg)
	}

	// Assert the structured log output for the panic
	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Expected ERROR log, got: %s", output)
	}
}

func TestSimpleEventBus_PanicRecovery(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger), events.WithWorkers(0))

	bus.Subscribe(func(ctx context.Context, e events.Event) {
		panic("boom")
	})

	err := bus.Publish(context.Background(), testEvent{typeName: "test_panic"})
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}

	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Expected ERROR log, got: %s", output)
	}
}

func TestSafePublish_Timeout(t *testing.T) {
	t.Parallel()

	// To test SafePublish failure due to full queue, we use a bus with workers that are slow.
	// But since workers are non-blocking (they sever), we'll just test that SafePublish 
	// returns an error if we cancel the context.
	bus := events.NewSimpleEventBus(context.Background())
	
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled

	err := events.SafePublish(ctx, bus, testEvent{typeName: "test_timeout"})
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}

	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got %v", err)
	}
}

func TestSimpleEventBus_FlushAndShutdown(t *testing.T) {
	t.Parallel()
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
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bus.Publish(ctx, testEvent{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSimpleEventBus_Race(t *testing.T) {
	nullLogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(nullLogger))
	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan struct{})

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

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			bus.Subscribe(func(ctx context.Context, e events.Event) {})
			time.Sleep(time.Microsecond)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestSimpleEventBus_Deadlock(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	ctx := context.Background()

	sub := &deadlockSubscriber{bus: bus}
	bus.SubscribeSubscriber("StatusUpdate", sub)

	go func() {
		for i := 0; i < 50; i++ {
			bus.Subscribe(func(ctx context.Context, e events.Event) {})
			time.Sleep(time.Microsecond)
		}
	}()

	done := make(chan error, 1)
	go func() {
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

type deadlockSubscriber struct {
	bus events.EventBus
}

func (s *deadlockSubscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Type() == "StatusUpdate" {
		return s.bus.Publish(ctx, events.TurnStarted{})
	}
	return nil
}

func TestSafePublish_Success(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	received := make(chan events.Event, 1)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		received <- e
	})

	err := events.SafePublish(context.Background(), bus, testEvent{val: "ok"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	select {
	case got := <-received:
		if got.(testEvent).val != "ok" {
			t.Errorf("expected ok, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventBus_RoutingErrorIsolation(t *testing.T) {
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger))

	errGlobal := errors.New("global error")
	errSpecific := errors.New("specific error")

	var globalCalled, specific1Called, specific2Called bool
	var mu sync.Mutex

	bus.SubscribeGlobal(&funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		globalCalled = true
		mu.Unlock()
		return errGlobal
	}})

	bus.SubscribeSubscriber("testEvent", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		specific1Called = true
		mu.Unlock()
		return errSpecific
	}})

	bus.SubscribeSubscriber("testEvent", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		specific2Called = true
		mu.Unlock()
		return nil
	}})

	_ = bus.Publish(context.Background(), testEvent{typeName: "testEvent"})
	_ = bus.Flush(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if !globalCalled || !specific1Called || !specific2Called {
		t.Error("not all subscribers were called")
	}

	output := buf.String()
	if !strings.Contains(output, "global error") || !strings.Contains(output, "specific error") {
		t.Errorf("expected errors in logs, got: %s", output)
	}
}

func TestEventTypes(t *testing.T) {
	events_list := []events.Event{
		events.StatusUpdate{},
		events.TurnStarted{},
		events.ResponseStreamEvent{},
		events.ToolCallEvent{},
		events.ToolResultEvent{},
		events.UsageMetricsEvent{},
		events.SystemMessageEvent{},
		events.TokenLimitReachedEvent{},
		events.SummarizationRequired{},
		events.TraceEvent{},
	}

	for _, e := range events_list {
		if e.Type() == "" {
			t.Errorf("empty type for %T", e)
		}
	}
}

func TestSafePublish_NoGoroutineLeak(t *testing.T) {
	initial := runtime.NumGoroutine()
	bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(1))

	sub := &respectfulSubscriber{}
	bus.SubscribeSubscriber("leak_test", sub)

	_ = bus.Publish(context.Background(), testEvent{typeName: "leak_test"})
	_ = bus.Shutdown(context.Background())

	time.Sleep(100 * time.Millisecond)
	final := runtime.NumGoroutine()

	if final > initial+15 {
		t.Errorf("Goroutine leak detected: initial %d, final %d", initial, final)
	}
}

type respectfulSubscriber struct{}

func (s *respectfulSubscriber) Handle(ctx context.Context, e events.Event) error {
	select {
	case <-time.After(1 * time.Hour):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestSafePublish_UncooperativeSubscriber(t *testing.T) {
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx,
		events.WithWorkers(2),
		events.WithQueueSize(200),
		events.WithMaxConcurrentSubscribers(2),
	)
	defer func() { _ = bus.Shutdown(ctx) }()

	initialGoroutines := runtime.NumGoroutine()

	block := make(chan struct{})
	sub := &uncooperativeSubscriber{block: block}
	bus.SubscribeSubscriber("uncooperative", sub)

	for i := 0; i < 50; i++ {
		_ = events.SafePublish(ctx, bus, testEvent{typeName: "uncooperative"})
	}

	time.Sleep(200 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	if finalGoroutines >= initialGoroutines+15 {
		t.Errorf("Bounded goroutine leak exceeded! Initial: %d, Final: %d. Expected at most ~%d", initialGoroutines, finalGoroutines, initialGoroutines+2)
	}

	close(block)
}

type uncooperativeSubscriber struct {
	block chan struct{}
}

func (s *uncooperativeSubscriber) Handle(ctx context.Context, e events.Event) error {
	<-s.block
	return nil
}

func TestWithLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger), events.WithWorkers(0))

	bus.SubscribeSubscriber("test_panic", &panicSubscriber{msg: "test panic"})
	_ = bus.Publish(context.Background(), testEvent{typeName: "test_panic"})

	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) || !strings.Contains(output, "test panic") {
		t.Errorf("Expected ERROR log with panic message, got: %s", output)
	}
}

func TestErrBusClosed_Explicit(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	_ = bus.Shutdown(context.Background())

	err := bus.Publish(context.Background(), testEvent{})
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}
