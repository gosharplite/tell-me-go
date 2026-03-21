// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events_test

import (
	"bytes"
	"context"
	"errors"
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

type blockingSubscriber struct {
	ready chan struct{}
	block chan struct{}
}

func (s *blockingSubscriber) Handle(ctx context.Context, e events.Event) error {
	close(s.ready)
	<-s.block
	return nil
}

type funcSubscriberWithErr struct {
	f func(context.Context, events.Event) error
}

func (s *funcSubscriberWithErr) Handle(ctx context.Context, e events.Event) error {
	return s.f(ctx, e)
}

func TestSimpleEventBus_PublishSubscribe(t *testing.T) {
	t.Parallel()
	bus := events.NewSimpleEventBus(context.Background())
	received := make(chan events.Event, 1)

	bus.Subscribe(func(ctx context.Context, e events.Event) {
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
	t.Parallel()

	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger))

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

	// Verify it contains multiple joined errors (Go 1.20+)
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Error("expected a joined error")
	} else if len(joined.Unwrap()) != 3 {
		t.Errorf("expected 3 joined errors, got %d", len(joined.Unwrap()))
	}

	// Assert the structured log output for the panic
	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Expected ERROR log, got: %s", output)
	}
	if !strings.Contains(output, `"panic_reason":"panic 1"`) {
		t.Errorf("Missing panic_reason key in log: %s", output)
	}
}

func TestSimpleEventBus_PanicRecovery(t *testing.T) {
	t.Parallel()

	// 1. Create an isolated, buffer-backed logger for this specific test
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	// 2. Inject the logger into the component
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger))

	bus.Subscribe(func(ctx context.Context, e events.Event) {
		panic("boom")
	})

	bus.Subscribe(func(ctx context.Context, e events.Event) {
		// Normal subscriber
	})

	err := bus.Publish(context.Background(), testEvent{typeName: "test_panic"})
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}

	if !strings.Contains(err.Error(), "subscriber panicked: boom") {
		t.Errorf("expected panic message in error, got %v", err)
	}

	// 4. Assert the structured log output
	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Expected ERROR log, got: %s", output)
	}
	if !strings.Contains(output, `"event_type":"test_panic"`) {
		t.Errorf("Missing event_type key in log: %s", output)
	}
	if !strings.Contains(output, `"stack_trace"`) {
		t.Errorf("Missing stack_trace in log: %s", output)
	}
}

func TestSimpleEventBus_PanicResilience(t *testing.T) {
	t.Parallel()

	// 1. Create an isolated, buffer-backed logger for this specific test
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	// 2. Inject the logger into the component
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger))

	results := make([]bool, 3)

	bus.SubscribeSubscriber("testEvent", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		results[0] = true
		return nil
	}})

	bus.SubscribeSubscriber("testEvent", &panicSubscriber{msg: "simulated UI crash"})

	bus.SubscribeSubscriber("testEvent", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		results[2] = true
		return nil
	}})

	err := bus.Publish(context.Background(), testEvent{typeName: "testEvent"})
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}

	if !results[0] {
		t.Error("Subscriber 1 did not execute")
	}
	if !results[2] {
		t.Error("Subscriber 3 did not execute")
	}

	if !strings.Contains(err.Error(), "simulated UI crash") {
		t.Errorf("expected panic message in error, got %v", err)
	}

	// 4. Assert the structured log output
	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Expected ERROR log, got: %s", output)
	}
	if !strings.Contains(output, `"subscriber_type":"*events_test.panicSubscriber"`) {
		t.Errorf("Missing subscriber_type key in log: %s", output)
	}
	if !strings.Contains(output, `"panic_reason":"simulated UI crash"`) {
		t.Errorf("Missing panic_reason key in log: %s", output)
	}
	if !strings.Contains(output, `"stack_trace"`) {
		t.Errorf("Missing stack_trace in log: %s", output)
	}
}

func TestSafePublish_Timeout(t *testing.T) {
	t.Parallel()

	// 1. Create an isolated, buffer-backed logger
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	// 2. Inject the logger into the component
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger))
	sub := &blockingSubscriber{
		ready: make(chan struct{}),
		block: make(chan struct{}),
	}
	bus.SubscribeSubscriber("test_timeout", sub)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := events.SafePublish(ctx, bus, testEvent{typeName: "test_timeout"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "publish timeout") {
		t.Errorf("expected timeout error message, got %v", err)
	}

	// 4. Assert the structured log output
	output := buf.String()
	if !strings.Contains(output, `"level":"WARN"`) {
		t.Errorf("Expected WARN log, got: %s", output)
	}
	if !strings.Contains(output, `"event_type":"test_timeout"`) {
		t.Errorf("Missing event_type key in log: %s", output)
	}
	if !strings.Contains(output, `"Event dropped due to publish timeout"`) {
		t.Errorf("Missing main log message: %s", output)
	}

	// Cleanup the blocking subscriber so the goroutine can finish
	close(sub.block)
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

	bus.Subscribe(func(ctx context.Context, e events.Event) {
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
			bus.Subscribe(func(ctx context.Context, e events.Event) {})
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
			bus.Subscribe(func(ctx context.Context, e events.Event) {})
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

func TestSafePublish_NilBus(t *testing.T) {
	err := events.SafePublish(context.Background(), nil, testEvent{})
	if !strings.Contains(err.Error(), "event bus is nil") {
		t.Errorf("expected error for nil bus, got %v", err)
	}
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

func TestSimpleEventBus_NilBusMethods(t *testing.T) {
	var b *events.SimpleEventBus
	if err := b.Publish(context.Background(), testEvent{}); err == nil {
		t.Error("expected error for nil bus Publish")
	}
	if err := b.Shutdown(context.Background()); err == nil {
		t.Error("expected error for nil bus Shutdown")
	}
	if err := b.Flush(context.Background()); err == nil {
		t.Error("expected error for nil bus Flush")
	}
	// Should not panic
	b.Subscribe(func(ctx context.Context, e events.Event) {})
	b.SubscribeGlobal(&errSubscriber{})
	b.SubscribeSubscriber("test", &errSubscriber{})
}

func TestSimpleEventBus_SubscribeClosed(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	_ = bus.Shutdown(context.Background())

	// Should not panic or register
	bus.Subscribe(func(ctx context.Context, e events.Event) {})
	bus.SubscribeGlobal(&errSubscriber{})
	bus.SubscribeSubscriber("test", &errSubscriber{})
}

func TestSimpleEventBus_FlushClosed(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	_ = bus.Shutdown(context.Background())
	if err := bus.Flush(context.Background()); !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestEventBus_GlobalRouting(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())

	var aCount, bCount, globalCount int
	var mu sync.Mutex

	// Register specific subscriber for EventTypeA
	bus.SubscribeSubscriber("EventTypeA", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		aCount++
		mu.Unlock()
		return nil
	}})

	// Register specific subscriber for EventTypeB
	bus.SubscribeSubscriber("EventTypeB", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		bCount++
		mu.Unlock()
		return nil
	}})

	// Register global subscriber
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		mu.Lock()
		globalCount++
		mu.Unlock()
	})

	ctx := context.Background()

	// Publish EventTypeA
	err := bus.Publish(ctx, testEvent{typeName: "EventTypeA"})
	if err != nil {
		t.Errorf("Publish A failed: %v", err)
	}

	// Publish EventTypeB
	err = bus.Publish(ctx, testEvent{typeName: "EventTypeB"})
	if err != nil {
		t.Errorf("Publish B failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if aCount != 1 {
		t.Errorf("expected EventTypeA subscriber to be called exactly once, got %d", aCount)
	}
	if bCount != 1 {
		t.Errorf("expected EventTypeB subscriber to be called exactly once, got %d", bCount)
	}
	if globalCount != 2 {
		t.Errorf("expected global subscriber to be called exactly twice, got %d", globalCount)
	}
}

func TestEventBus_RoutingErrorIsolation(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())

	errGlobal := errors.New("global error")
	errSpecific := errors.New("specific error")

	var globalCalled, specific1Called, specific2Called bool
	var mu sync.Mutex

	// 1. Global subscriber that intentionally returns an error
	bus.SubscribeGlobal(&funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		globalCalled = true
		mu.Unlock()
		return errGlobal
	}})

	// 2. Specific subscriber that intentionally returns an error
	bus.SubscribeSubscriber("testEvent", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		specific1Called = true
		mu.Unlock()
		return errSpecific
	}})

	// 3. Second specific subscriber that succeeds
	bus.SubscribeSubscriber("testEvent", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		specific2Called = true
		mu.Unlock()
		return nil
	}})

	err := bus.Publish(context.Background(), testEvent{typeName: "testEvent"})

	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	mu.Lock()
	defer mu.Unlock()

	if !globalCalled {
		t.Error("global subscriber was not called")
	}
	if !specific1Called {
		t.Error("specific subscriber 1 was not called")
	}
	if !specific2Called {
		t.Error("specific subscriber 2 was not called")
	}

	if !errors.Is(err, errGlobal) {
		t.Errorf("expected error to contain global error, got: %v", err)
	}
	if !errors.Is(err, errSpecific) {
		t.Errorf("expected error to contain specific error, got: %v", err)
	}

	// Verify it contains multiple joined errors
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Error("expected a joined error")
	} else if len(joined.Unwrap()) != 2 {
		t.Errorf("expected 2 joined errors, got %d", len(joined.Unwrap()))
	}
}

func TestSimpleEventBus_SubscribeNil(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	// Should not panic or register
	bus.Subscribe(nil)
	bus.SubscribeGlobal(nil)
	bus.SubscribeSubscriber("test", nil)
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
		events.ConfigUpdated{},
		events.TurnStatusEvent{},
	}

	for _, e := range events_list {
		if e.Type() == "" {
			t.Errorf("empty type for %T", e)
		}
	}
}

func TestSafePublish_InternalTimeout(t *testing.T) {
	t.Parallel()

	bus := events.NewSimpleEventBus(context.Background())
	sub := &blockingSubscriber{
		ready: make(chan struct{}),
		block: make(chan struct{}),
	}
	bus.SubscribeSubscriber("test_internal_timeout", sub)

	// SafePublish with context.Background() should NOT block forever due to internal 2s timeout
	start := time.Now()
	err := events.SafePublish(context.Background(), bus, testEvent{typeName: "test_internal_timeout"})
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected internal timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "publish timeout") {
		t.Errorf("expected timeout error message, got %v", err)
	}

	// We expect roughly 2s, give it some slack for slow CI
	if duration < 1900*time.Millisecond || duration > 3000*time.Millisecond {
		t.Errorf("expected ~2s timeout, got %v", duration)
	}

	// Cleanup
	close(sub.block)
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

func TestSafePublish_NoGoroutineLeak(t *testing.T) {
	// 1. Capture the initial number of goroutines
	initial := runtime.NumGoroutine()

	bus := events.NewSimpleEventBus(context.Background())

	// 2. Create a mock subscriber that simulates a long-running task but respects ctx.Done()
	sub := &respectfulSubscriber{}
	bus.SubscribeSubscriber("leak_test", sub)

	// 3. Call SafePublish with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := events.SafePublish(ctx, bus, testEvent{typeName: "leak_test"})

	// 4. Assert that SafePublish returns a timeout error
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// 5. Add a small delay to allow the scheduler to clean up the anonymous goroutine.
	time.Sleep(100 * time.Millisecond)

	// 6. Capture runtime.NumGoroutine() again and assert that it has not increased
	final := runtime.NumGoroutine()

	if final > initial {
		t.Errorf("Goroutine leak detected: initial %d, final %d", initial, final)
	}
}

func TestWithLogger(t *testing.T) {
	t.Parallel()

	// 1. Create an isolated, buffer-backed logger
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	// 2. Inject the logger into the component via functional option
	bus := events.NewSimpleEventBus(context.Background(), events.WithLogger(testLogger))

	// 3. Register a subscriber that panics to trigger the logger
	bus.SubscribeSubscriber("test_panic", &panicSubscriber{msg: "test panic"})

	// 4. Publish an event to trigger the subscriber
	_ = bus.Publish(context.Background(), testEvent{typeName: "test_panic"})

	// 5. Assert that the EventBus caught the panic and logged it at ERROR level
	output := buf.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Errorf("Expected ERROR log in buffer, got: %s", output)
	}
	if !strings.Contains(output, "test panic") {
		t.Errorf("Expected panic message in log, got: %s", output)
	}
}

func TestErrBusClosed_Explicit(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background())
	_ = bus.Shutdown(context.Background())

	err := bus.Publish(context.Background(), testEvent{})
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}

	err = bus.Flush(context.Background())
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}
