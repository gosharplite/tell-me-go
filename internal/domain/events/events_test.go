// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

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
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)
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
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	err1 := errors.New("err 1")
	err2 := errors.New("err 2")

	bus.SubscribeSubscriber("testEvent", &errSubscriber{err: err1})
	bus.SubscribeSubscriber("testEvent", &errSubscriber{err: err2})
	bus.SubscribeSubscriber("testEvent", &panicSubscriber{msg: "panic 1"})

	err := bus.Publish(ctx, testEvent{typeName: "testEvent"})
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
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	bus.Subscribe(func(ctx context.Context, e events.Event) {
		panic("boom")
	})

	err := bus.Publish(ctx, testEvent{typeName: "test_panic"})
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
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	ctx2, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled

	err := events.SafePublish(ctx2, bus, testEvent{typeName: "test_timeout"})
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}

	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got %v", err)
	}
}

func TestSimpleEventBus_FlushAndShutdown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))

	if err := bus.Flush(ctx); err != nil {
		t.Errorf("Flush failed: %v", err)
	}

	if err := bus.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	if err := bus.Publish(ctx, testEvent{}); !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestSimpleEventBus_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctxRoot := context.Background()
	bus := events.NewSimpleEventBus(ctxRoot, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	ctx, cancel := context.WithCancel(ctxRoot)
	cancel()

	err := bus.Publish(ctx, testEvent{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSimpleEventBus_Race(t *testing.T) {
	nullLogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(nullLogger), events.WithWorkers(2))
	inframock.CleanupBus(t, bus)

	var wg sync.WaitGroup

	// Publisher loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = bus.Publish(ctx, events.StatusUpdate{Message: "test"})
		}
	}()

	// Subscriber loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			bus.Subscribe(func(ctx context.Context, e events.Event) {})
		}
	}()

	wg.Wait()
}

func TestSimpleEventBus_Deadlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	sub := &deadlockSubscriber{bus: bus}
	bus.SubscribeSubscriber("StatusUpdate", sub)

	go func() {
		for i := 0; i < 100; i++ {
			bus.Subscribe(func(ctx context.Context, e events.Event) {})
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
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)
	received := make(chan events.Event, 1)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		received <- e
	})

	err := events.SafePublish(ctx, bus, testEvent{val: "ok"})
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
	t.Parallel()
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithWorkers(1))
	inframock.CleanupBus(t, bus)

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

	_ = bus.Publish(ctx, testEvent{typeName: "testEvent"})
	_ = bus.Flush(ctx)

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
	t.Parallel()
	events_list := []events.Event{
		events.StatusUpdate{},
		events.TurnStarted{},
		events.InferenceStartedEvent{},
		events.ResponseEvent{},
		events.ToolCallEvent{},
		events.ToolResultEvent{},
		events.UsageMetricsEvent{},
		events.SystemMessageEvent{},
		events.TokenLimitReachedEvent{},
		events.SummarizationRequired{},
		events.TraceEvent{},
		events.SummarizationStartedEvent{},
		events.RetryWaitingEvent{},
		events.ConsentStartedEvent{},
		events.ConsentFinishedEvent{},
	}

	for _, e := range events_list {
		if e.Type() == "" {
			t.Errorf("empty type for %T", e)
		}
	}
}

func TestSafePublish_NoGoroutineLeak(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(1))
	inframock.CleanupBus(t, bus)

	sub := &respectfulSubscriber{}
	bus.SubscribeSubscriber("leak_test", sub)

	_ = bus.Publish(ctx, testEvent{typeName: "leak_test"})
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
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx,
		events.WithWorkers(2),
		events.WithQueueSize(200),
		events.WithMaxConcurrentSubscribers(2),
	)
	inframock.CleanupBus(t, bus)

	block := make(chan struct{})
	sub := &uncooperativeSubscriber{block: block}
	bus.SubscribeSubscriber("uncooperative", sub)

	for i := 0; i < 50; i++ {
		_ = events.SafePublish(ctx, bus, testEvent{typeName: "uncooperative"})
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
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	bus.SubscribeSubscriber("test_panic", &panicSubscriber{msg: "test panic"})
	_ = bus.Publish(ctx, testEvent{typeName: "test_panic"})

	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) || !strings.Contains(output, "test panic") {
		t.Errorf("Expected ERROR log with panic message, got: %s", output)
	}
}

func TestErrBusClosed_Explicit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	_ = bus.Shutdown(ctx)

	err := bus.Publish(ctx, testEvent{})
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestSimpleEventBus_HOLBlocking(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf inframock.SafeBuffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	// Small semaphore and single worker to trigger HOL blocking
	bus := events.NewSimpleEventBus(ctx,
		events.WithLogger(testLogger),
		events.WithWorkers(1),
		events.WithMaxConcurrentSubscribers(1),
		events.WithQueueSize(10),
	)
	inframock.CleanupBus(t, bus)

	block := make(chan struct{})
	slowSub := &uncooperativeSubscriber{block: block}
	bus.SubscribeGlobal(slowSub)

	// Publish first event - will occupy the single semaphore slot
	_ = bus.Publish(ctx, testEvent{typeName: "E1"})

	// Wait a bit to ensure E1 is picked up by the worker and occupies the slot
	time.Sleep(100 * time.Millisecond)

	// Publish second event - should trigger timeout in dispatch (500ms) because E1 holds the slot
	_ = bus.Publish(ctx, testEvent{typeName: "E2"})

	// Publish third event - should be picked up by the worker after E2 times out
	_ = bus.Publish(ctx, testEvent{typeName: "E3"})

	// Give enough time for timeouts (2 * 500ms + some buffer)
	time.Sleep(2 * time.Second)

	output := buf.String()

	// Verify log contains warning for E2 and E3 being skipped
	if !strings.Contains(output, "Subscriber saturated; event skipped for this subscriber") {
		t.Errorf("Expected warning log not found in: %s", output)
	}

	if !strings.Contains(output, "event_type\":\"E2\"") {
		t.Errorf("Expected E2 to be skipped and logged, output: %s", output)
	}

	if !strings.Contains(output, "event_type\":\"E3\"") {
		t.Errorf("Expected E3 to be skipped and logged (was it blocked?), output: %s", output)
	}

	// Unblock E1
	close(block)
}
