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
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"go.uber.org/goleak"
	"golang.org/x/sync/errgroup"
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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
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
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

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
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))

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
	bus := events.NewSimpleEventBus(ctxRoot, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	ctx, cancel := context.WithCancel(ctxRoot)
	cancel()

	err := bus.Publish(ctx, testEvent{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSimpleEventBus_Race(t *testing.T) {
	nullLogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithLogger(nullLogger), events.WithAsync(true))
	eventstest.CleanupBus(t, bus)

	var wg sync.WaitGroup

	// Coordinated background listener
	// We don't add this to wg because we want it to run until we cancel the context
	// after all other tasks are done.
	go func() {
		if err := bus.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("bus.Listen returned error: %v", err)
		}
	}()

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
	cancel() // Explicitly stop Listen
}

func TestSimpleEventBus_Deadlock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
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
	g, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(true))
	eventstest.CleanupBus(t, bus)

	g.Go(func() error {
		return bus.Listen(ctx)
	})

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
	if !globalCalled || !specific1Called || !specific2Called {
		mu.Unlock()
		t.Error("not all subscribers were called")
	} else {
		mu.Unlock()
	}

	cancel() // Stop Listen
	_ = g.Wait()

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
		events.ConfigUpdated{},
		events.TurnStatusEvent{},
		events.ToolExecutionStartedEvent{},
	}

	for _, e := range events_list {
		if e.Type() == "" {
			t.Errorf("empty type for %T", e)
		}
	}
}

func TestSimpleEventBus_SubscriptionRouting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Use synchronous bus for deterministic results
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	var (
		globalReceived      []events.Event
		statusReceived      []events.Event
		turnStartedReceived []events.Event
		mu                  sync.Mutex
	)

	// Subscriber for all events
	bus.SubscribeGlobal(&funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		globalReceived = append(globalReceived, e)
		mu.Unlock()
		return nil
	}})

	// Subscriber for specific event type "StatusUpdate"
	bus.SubscribeSubscriber("StatusUpdate", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		statusReceived = append(statusReceived, e)
		mu.Unlock()
		return nil
	}})

	// Subscriber for specific event type "TurnStarted"
	bus.SubscribeSubscriber("TurnStarted", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		turnStartedReceived = append(turnStartedReceived, e)
		mu.Unlock()
		return nil
	}})

	// Publish multiple types
	_ = bus.Publish(ctx, events.StatusUpdate{Message: "status 1"})
	_ = bus.Publish(ctx, events.TurnStarted{Turn: 1})
	_ = bus.Publish(ctx, events.StatusUpdate{Message: "status 2"})
	_ = bus.Publish(ctx, testEvent{typeName: "Other"})

	mu.Lock()
	defer mu.Unlock()

	// Verify global received everything
	if len(globalReceived) != 4 {
		t.Errorf("expected global to receive 4 events, got %d", len(globalReceived))
	}

	// Verify status subscriber
	if len(statusReceived) != 2 {
		t.Errorf("expected status subscriber to receive 2 events, got %d", len(statusReceived))
	}
	for _, e := range statusReceived {
		if e.Type() != "StatusUpdate" {
			t.Errorf("status subscriber received wrong event type: %s", e.Type())
		}
	}

	// Verify turn started subscriber
	if len(turnStartedReceived) != 1 {
		t.Errorf("expected turn started subscriber to receive 1 event, got %d", len(turnStartedReceived))
	}
	if turnStartedReceived[0].Type() != "TurnStarted" {
		t.Errorf("turn started subscriber received wrong event type: %s", turnStartedReceived[0].Type())
	}
}

func TestSafePublish_NoGoroutineLeak(t *testing.T) {
	t.Parallel()
	g, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
	eventstest.CleanupBus(t, bus)

	g.Go(func() error {
		return bus.Listen(ctx)
	})

	sub := &respectfulSubscriber{}
	bus.SubscribeSubscriber("leak_test", sub)

	_ = bus.Publish(ctx, testEvent{typeName: "leak_test"})

	// Trigger shutdown and verify clean exit via goleak (in TestMain)
	cancel()
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Listen failed: %v", err)
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
	t.Parallel()
	g, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	bus := events.NewSimpleEventBus(ctx,
		events.WithAsync(true),
		events.WithQueueSize(200),
		events.WithMaxConcurrentSubscribers(2),
	)
	eventstest.CleanupBus(t, bus)

	g.Go(func() error {
		return bus.Listen(ctx)
	})

	block := make(chan struct{})
	sub := &uncooperativeSubscriber{block: block}
	bus.SubscribeSubscriber("uncooperative", sub)

	for i := 0; i < 50; i++ {
		_ = events.SafePublish(ctx, bus, testEvent{typeName: "uncooperative"})
	}

	close(block)
	cancel()
	_ = g.Wait()
}

type uncooperativeSubscriber struct {
	block             <-chan struct{}
	startedProcessing chan struct{}
}

func (s *uncooperativeSubscriber) Handle(ctx context.Context, e events.Event) error {
	if s.startedProcessing != nil {
		select {
		case s.startedProcessing <- struct{}{}:
		default:
		}
	}
	<-s.block
	return nil
}

func TestWithLogger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	_ = bus.Shutdown(ctx)

	err := bus.Publish(ctx, testEvent{})
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestEventBus_SlowSubscriber(t *testing.T) {
	t.Parallel()
	g, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	buf := testfixtures.NewSafeBuffer()
	testLogger := slog.New(slog.NewJSONHandler(buf, nil))

	// Setup bus with tiny queue to force backpressure quickly
	bus := events.NewSimpleEventBus(ctx,
		events.WithLogger(testLogger),
		events.WithAsync(true),
		events.WithQueueSize(1),
	)
	eventstest.CleanupBus(t, bus)

	g.Go(func() error {
		return bus.Listen(ctx)
	})

	block := make(chan struct{})
	startedProcessing := make(chan struct{}, 1)
	slowSub := &uncooperativeSubscriber{block: block, startedProcessing: startedProcessing}
	bus.SubscribeGlobal(slowSub)

	// Fill the subscriber's channel
	_ = bus.Publish(ctx, testEvent{typeName: "E1"})
	// Wait for worker to pick up E1 and block
	<-slowSub.startedProcessing

	_ = bus.Publish(ctx, testEvent{typeName: "E2"}) // This will sit in the queue (size 1)

	// Now publish E3. The subscriber's queue is full.
	// Publish MUST return immediately and drop the event.
	start := time.Now()
	err := bus.Publish(ctx, testEvent{typeName: "E3"})
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected Publish to return nil despite load shedding, got: %v", err)
	}

	if duration > 500*time.Millisecond {
		t.Errorf("Publish took too long: %v. It must not block the publisher.", duration)
	}

	output := buf.String()

	// Verify log contains warning for dropping E3
	if !strings.Contains(output, "subscriber queue full, dropping event") {
		t.Errorf("Expected load shedding log warning, got: %s", output)
	}
	if !strings.Contains(output, "event_type\":\"E3\"") {
		t.Errorf("Expected E3 to be logged as dropped, output: %s", output)
	}

	// Unblock E1
	close(block)

	cancel()
	_ = g.Wait()
}

func TestEventBus_DefensiveErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrBusNotInitialized", func(t *testing.T) {
		var bus *events.SimpleEventBus = nil
		ctx := context.Background()

		errPublish := bus.Publish(ctx, testEvent{typeName: "nil_test"})
		if !errors.Is(errPublish, events.ErrBusNotInitialized) {
			t.Errorf("expected Publish on nil bus to return ErrBusNotInitialized, got %v", errPublish)
		}

		errFlush := bus.Flush(ctx)
		if !errors.Is(errFlush, events.ErrBusNotInitialized) {
			t.Errorf("expected Flush on nil bus to return ErrBusNotInitialized, got %v", errFlush)
		}

		errShutdown := bus.Shutdown(ctx)
		if !errors.Is(errShutdown, events.ErrBusNotInitialized) {
			t.Errorf("expected Shutdown on nil bus to return ErrBusNotInitialized, got %v", errShutdown)
		}

		errSafePublish := events.SafePublish(ctx, bus, testEvent{typeName: "nil_test"})
		if !errors.Is(errSafePublish, events.ErrBusNotInitialized) {
			t.Errorf("expected SafePublish on nil bus to return ErrBusNotInitialized, got %v", errSafePublish)
		}
	})

	t.Run("ErrBusClosed", func(t *testing.T) {
		ctx := context.Background()
		bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
		eventstest.CleanupBus(t, bus)

		errShutdown1 := bus.Shutdown(ctx)
		if errShutdown1 != nil {
			t.Fatalf("first shutdown failed: %v", errShutdown1)
		}

		// The second shutdown should be a no-op (graceful), wait, let's check Shutdown behavior.
		errShutdown2 := bus.Shutdown(ctx)
		if errShutdown2 != nil {
			t.Errorf("second shutdown should be no-op, got %v", errShutdown2)
		}

		errPublish := bus.Publish(ctx, testEvent{typeName: "closed_test"})
		if !errors.Is(errPublish, events.ErrBusClosed) {
			t.Errorf("expected Publish on closed bus to return ErrBusClosed, got %v", errPublish)
		}

		errFlush := bus.Flush(ctx)
		if !errors.Is(errFlush, events.ErrBusClosed) {
			t.Errorf("expected Flush on closed bus to return ErrBusClosed, got %v", errFlush)
		}
	})
}

func TestEventBus_SimpleSnapshot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
	eventstest.CleanupBus(t, bus)

	received := make(chan events.Event, 1)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		received <- e
	})

	go func() {
		if err := bus.Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("bus.Listen returned error: %v", err)
		}
	}()

	err := bus.Publish(ctx, testEvent{val: "snapshot_test"})
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case got := <-received:
		if got.(testEvent).val != "snapshot_test" {
			t.Errorf("expected snapshot_test, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSimpleEventBus_SubscriptionVariations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	var (
		globalReceived []events.Event
		typeReceived   []events.Event
		funcReceived   []events.Event
		mu             sync.Mutex
	)

	// 1. SubscribeGlobal (Subscriber interface)
	bus.SubscribeGlobal(&funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		globalReceived = append(globalReceived, e)
		mu.Unlock()
		return nil
	}})

	// 2. SubscribeSubscriber (Subscriber interface) for specific type
	bus.SubscribeSubscriber("test_type", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		typeReceived = append(typeReceived, e)
		mu.Unlock()
		return nil
	}})

	// 3. Subscribe (func) for same specific type
	// Note: Subscribe is actually global in current implementation!
	// Let's re-read events.go:
	/*
		func (b *SimpleEventBus) Subscribe(sub func(context.Context, Event)) {
			// ...
			w := b.newWrapper(&funcSubscriber{f: sub})
			b.globalSubscribers = append(b.globalSubscribers, w)
		}
	*/
	// Yes, Subscribe is global. So "multiple subscribers for the same type"
	// should be tested using SubscribeSubscriber multiple times.

	bus.SubscribeSubscriber("test_type", &funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		funcReceived = append(funcReceived, e)
		mu.Unlock()
		return nil
	}})

	// Publish event of specific type
	ev1 := testEvent{typeName: "test_type", val: "v1"}
	_ = bus.Publish(ctx, ev1)

	// Publish event of another type
	ev2 := testEvent{typeName: "other_type", val: "v2"}
	_ = bus.Publish(ctx, ev2)

	mu.Lock()
	defer mu.Unlock()

	if len(globalReceived) != 2 {
		t.Errorf("expected global to receive 2 events, got %d", len(globalReceived))
	}

	if len(typeReceived) != 1 {
		t.Errorf("expected type subscriber to receive 1 event, got %d", len(typeReceived))
	}
	if typeReceived[0].(testEvent).val != "v1" {
		t.Errorf("expected v1, got %v", typeReceived[0].(testEvent).val)
	}

	if len(funcReceived) != 1 {
		t.Errorf("expected second type subscriber to receive 1 event, got %d", len(funcReceived))
	}
}

func TestSimpleEventBus_ShutdownAndFlush(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true), events.WithQueueSize(10))

	// Start background workers
	g, listenCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return bus.Listen(listenCtx)
	})
	bus.WaitStarted()

	var processedCount int
	var mu sync.Mutex
	processed := make(chan struct{}, 5)

	bus.SubscribeGlobal(&funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		processedCount++
		mu.Unlock()
		processed <- struct{}{}
		return nil
	}})

	// Publish multiple events
	numEvents := 5
	for i := 0; i < numEvents; i++ {
		if err := bus.Publish(ctx, testEvent{val: i}); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
	}

	// Call Flush() and verify all pending events were processed
	flushCtx, flushCancel := context.WithTimeout(ctx, 1*time.Second)
	defer flushCancel()
	if err := bus.Flush(flushCtx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Drain processed signals to confirm each event was handled
	for i := 0; i < numEvents; i++ {
		select {
		case <-processed:
		case <-time.After(2 * time.Second):
			t.Fatal("subscriber did not process event in time")
		}
	}

	mu.Lock()
	if processedCount != numEvents {
		t.Errorf("expected %d events to be processed after Flush, got %d", numEvents, processedCount)
	}
	mu.Unlock()

	// Call Shutdown() and verify it works
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer shutdownCancel()
	if err := bus.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Verify it returns context.Canceled if the context is canceled
	bus2 := events.NewSimpleEventBus(context.Background(), events.WithAsync(true))
	canceledCtx, canceledCancel := context.WithCancel(context.Background())
	canceledCancel()
	if err := bus2.Shutdown(canceledCtx); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	cancel() // Stop Listen
	_ = g.Wait()
}

func TestSimpleEventBus_FlushCancellations(t *testing.T) {
	t.Parallel()

	t.Run("ContextCanceled", func(t *testing.T) {
		ctx := context.Background()
		bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
		eventstest.CleanupBus(t, bus)

		// Create a slow subscriber to keep pendingCount > 0
		bus.SubscribeGlobal(&uncooperativeSubscriber{block: make(chan struct{})})
		_ = bus.Publish(ctx, testEvent{typeName: "slow"})

		flushCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		err := bus.Flush(flushCtx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("BusClosedDuringFlush", func(t *testing.T) {
		ctx := context.Background()
		bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))

		// Start the listener so a worker picks up events.
		listenCtx, listenCancel := context.WithCancel(ctx)
		listenDone := make(chan struct{})
		go func() {
			defer close(listenDone)
			if err := bus.Listen(listenCtx); err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("Listen returned: %v", err)
			}
		}()
		bus.WaitStarted()

		// Create a slow subscriber that signals when the worker is blocked inside Handle.
		block := make(chan struct{})
		started := make(chan struct{}, 1)
		bus.SubscribeGlobal(&uncooperativeSubscriber{
			block:             block,
			startedProcessing: started,
		})
		_ = bus.Publish(ctx, testEvent{typeName: "slow"})

		// Wait for the worker goroutine to pick up the event and block in Handle.
		select {
		case <-started:
		case <-time.After(1 * time.Second):
			t.Fatal("worker did not start processing")
		}

		// Now the worker is blocked in Handle (pendingCount > 0). Flush will
		// deterministically enter its wait loop.
		errChan := make(chan error, 1)
		go func() {
			errChan <- bus.Flush(ctx)
		}()

		// Shutdown cancels the bus context (which unblocks Flush) and then waits
		// for workers to drain. Since our worker is blocked in Handle, Shutdown
		// would hang. Run it in a separate goroutine so we can observe Flush's
		// result first, then unblock the worker to let Shutdown complete.
		shutdownDone := make(chan struct{})
		go func() {
			defer close(shutdownDone)
			_ = bus.Shutdown(ctx)
		}()

		err := <-errChan
		if !errors.Is(err, events.ErrBusClosed) {
			t.Errorf("expected ErrBusClosed, got %v", err)
		}

		// Cleanup: unblock the subscriber so the worker drains, allowing
		// Shutdown and Listen to return cleanly.
		close(block)
		listenCancel()
		<-listenDone
		<-shutdownDone
	})
}

// TestSimpleEventBus_Flush_BusContextCancellation exercises the <-b.ctx.Done()
// path in Flush's dual select (line 109-110 of event_bus_lifecycle.go).
//
// The existing BusClosedDuringFlush test publishes only 1 event, so
// decPending() fires before the subscriber blocks — pendingCount drops
// to 0, flushWaiter closes done immediately, and the select picks <-done
// instead of <-b.ctx.Done(). This test publishes 2 events to keep
// pendingCount > 0 while a worker is blocked, ensuring the Flush select
// is forced into the <-b.ctx.Done() branch when Shutdown cancels the bus.
func TestSimpleEventBus_Flush_BusContextCancellation(t *testing.T) {
	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true), events.WithQueueSize(2))

	// Start the listener so workers pick up events.
	listenCtx, listenCancel := context.WithCancel(ctx)
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		if err := bus.Listen(listenCtx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("Listen returned: %v", err)
		}
	}()
	bus.WaitStarted()

	// Create a slow subscriber that signals when the worker is blocked.
	block := make(chan struct{})
	started := make(chan struct{}, 2)
	bus.SubscribeGlobal(&uncooperativeSubscriber{
		block:             block,
		startedProcessing: started,
	})

	// Publish 2 events. The worker dequeues event #1, calls decPending()
	// (pendingCount goes from 2 to 1), then blocks in Handle. Event #2
	// stays in the queue, so pendingCount remains at 1.
	if err := bus.Publish(ctx, testEvent{typeName: "slow-1"}); err != nil {
		close(block)
		listenCancel()
		t.Fatalf("Publish event 1 failed: %v", err)
	}
	if err := bus.Publish(ctx, testEvent{typeName: "slow-2"}); err != nil {
		close(block)
		listenCancel()
		t.Fatalf("Publish event 2 failed: %v", err)
	}

	// Wait for the worker to pick up event #1 and block in Handle.
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("worker did not start processing")
	}

	// Flush in a goroutine. With pendingCount > 0, flushWaiter enters
	// the cond.Wait() loop instead of closing done immediately.
	errChan := make(chan error, 1)
	go func() {
		errChan <- bus.Flush(ctx)
	}()

	// Shutdown cancels the bus context (b.ctx), which triggers
	// <-b.ctx.Done() in Flush's select.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		_ = bus.Shutdown(ctx)
	}()

	err := <-errChan
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed from <-b.ctx.Done() path, got %v", err)
	}

	// Cleanup: unblock the subscriber so the worker drains, allowing
	// Shutdown and Listen to return cleanly without goroutine leaks.
	close(block)
	listenCancel()
	<-listenDone
	<-shutdownDone
}

func TestSimpleEventBus_ListenDefensive_NilBus(t *testing.T) {
	t.Parallel()

	var bus *events.SimpleEventBus = nil
	err := bus.Listen(context.Background())
	if !errors.Is(err, events.ErrBusNotInitialized) {
		t.Errorf("expected ErrBusNotInitialized, got %v", err)
	}
}

func TestSimpleEventBus_ListenDefensive_ClosedBus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
	_ = bus.Shutdown(ctx)

	err := bus.Listen(ctx)
	if !errors.Is(err, events.ErrBusClosed) {
		t.Errorf("expected ErrBusClosed, got %v", err)
	}
}

func TestSimpleEventBus_ListenDefensive_AlreadyRunning(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
	eventstest.CleanupBus(t, bus)

	// Start the first Listen in a background goroutine and wait for
	// full initialization. After WaitStarted returns, b.running is
	// guaranteed true under the lock.
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = bus.Listen(ctx)
	}()
	bus.WaitStarted()

	// Call Listen a second time on an already-running bus.
	// This exercises the early-return path:
	//   if b.running { b.mu.Unlock(); return nil }
	err := bus.Listen(ctx)
	if err != nil {
		t.Errorf("expected nil on re-entrant Listen to already-running bus, got %v", err)
	}

	// Clean shutdown: cancel the context and wait for the first Listen
	// goroutine to return.
	cancel()
	<-listenDone
}

func TestSimpleEventBus_ListenDefensive_SyncMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	// Listen on a sync-mode bus must return nil immediately
	// and signal started so WaitStarted does not block.
	err := bus.Listen(ctx)
	if err != nil {
		t.Errorf("Listen in sync mode should return nil, got %v", err)
	}

	// WaitStarted must return immediately — proves signalStarted was called.
	// Use a channel + timeout to detect deadlock without hanging the test.
	done := make(chan struct{})
	go func() {
		bus.WaitStarted()
		close(done)
	}()
	select {
	case <-done:
		// OK — WaitStarted returned
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitStarted blocked after sync-mode Listen — signalStarted was not called")
	}

	// Publish should still work after sync-mode Listen (bus is not closed).
	received := make(chan events.Event, 1)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		received <- e
	})
	if err := bus.Publish(ctx, testEvent{val: "sync-mode"}); err != nil {
		t.Fatalf("Publish after sync-mode Listen failed: %v", err)
	}
	select {
	case got := <-received:
		if got.(testEvent).val != "sync-mode" {
			t.Errorf("expected 'sync-mode', got %v", got.(testEvent).val)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event after sync-mode Listen")
	}
}

func TestSimpleEventBus_NilAndClosedSubscriptions(t *testing.T) {
	t.Parallel()

	t.Run("NilBus", func(t *testing.T) {
		var b *events.SimpleEventBus = nil
		b.Subscribe(nil)
		b.SubscribeGlobal(nil)
		b.SubscribeSubscriber("test", nil)
		// Should not panic
	})

	t.Run("NilSubscribers", func(t *testing.T) {
		ctx := context.Background()
		b := events.NewSimpleEventBus(ctx)
		eventstest.CleanupBus(t, b)
		b.Subscribe(nil)
		b.SubscribeGlobal(nil)
		b.SubscribeSubscriber("test", nil)
		// Should not panic or add nil subscribers
	})

	t.Run("ClosedBus", func(t *testing.T) {
		ctx := context.Background()
		b := events.NewSimpleEventBus(ctx)
		_ = b.Shutdown(ctx)

		b.Subscribe(func(ctx context.Context, e events.Event) {})
		b.SubscribeGlobal(&errSubscriber{})
		b.SubscribeSubscriber("test", &errSubscriber{})
		// Should not add subscribers
	})

	t.Run("SafePublishLiteralNil", func(t *testing.T) {
		err := events.SafePublish(context.Background(), nil, testEvent{})
		if !errors.Is(err, events.ErrBusNotInitialized) {
			t.Errorf("expected ErrBusNotInitialized, got %v", err)
		}
	})
}

// TestSimpleEventBus_Shutdown_FlushTimeout exercises the ctx.Done() path in
// Shutdown where the worker goroutines do not drain before the context expires.
func TestSimpleEventBus_Shutdown_FlushTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
	eventstest.CleanupBus(t, bus)

	// A subscriber that blocks forever — keeps workerWG from draining.
	started := make(chan struct{}, 1)
	foreverBlock := make(chan struct{})
	bus.SubscribeGlobal(&uncooperativeSubscriber{
		block:             foreverBlock,
		startedProcessing: started,
	})

	// Start the listener with a cancellable context so workers can drain.
	listenCtx, listenCancel := context.WithCancel(ctx)
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		if err := bus.Listen(listenCtx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("Listen returned: %v", err)
		}
	}()

	// Publish an event so a worker goroutine enters the blocking Handle.
	if err := bus.Publish(ctx, testEvent{typeName: "keep_worker_busy"}); err != nil {
		close(foreverBlock)
		listenCancel()
		t.Fatalf("Publish failed: %v", err)
	}

	// Wait deterministically for worker to enter Handle
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("worker did not start processing")
	}

	// Shutdown with an already-cancelled context — forces the ctx.Done() branch.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	err := bus.Shutdown(cancelCtx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from Shutdown timeout, got %v", err)
	}

	// Cleanup: unblock the subscriber and cancel the listen context so
	// workers drain and the test does not leak goroutines.
	close(foreverBlock)
	listenCancel()
	<-listenDone
}

// drainSignals drains exactly count signals from ch, failing the test if
// any signal does not arrive within the timeout.
func drainSignals(t *testing.T, ch <-chan struct{}, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("subscriber did not process event in time")
		}
	}
}

// TestSimpleEventBus_Shutdown_ActiveWorkers exercises the <-done path in
// Shutdown where the worker goroutines drain successfully before the context
// expires, and close(done) fires from the normal path (not recover).
func TestSimpleEventBus_Shutdown_ActiveWorkers(t *testing.T) {
	// Not parallel — uses goleak-sensitive lifecycle.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true), events.WithQueueSize(10))
	eventstest.CleanupBus(t, bus)

	// Subscribe BEFORE starting Listen so the subscriber is registered when
	// Listen collects wrappers and starts workers.
	var processedCount int
	var mu sync.Mutex
	processed := make(chan struct{}, 10)
	bus.SubscribeGlobal(&funcSubscriberWithErr{f: func(ctx context.Context, e events.Event) error {
		mu.Lock()
		processedCount++
		mu.Unlock()
		processed <- struct{}{}
		return nil
	}})

	// Use a cancellable listen context so we can cleanly stop the listener.
	listenCtx, listenCancel := context.WithCancel(ctx)
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		if err := bus.Listen(listenCtx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("Listen returned: %v", err)
		}
	}()

	// Wait for Listen to fully initialize.
	bus.WaitStarted()

	// Publish several events so workers are busy.
	numEvents := 5
	for i := 0; i < numEvents; i++ {
		if err := bus.Publish(ctx, testEvent{val: i}); err != nil {
			t.Fatalf("Publish %d failed: %v", i, err)
		}
	}

	// Drain processed signals to confirm each event was handled
	drainSignals(t, processed, numEvents)

	// Flush ensures all events are processed before we stop the listener.
	flushCtx, flushCancel := context.WithTimeout(ctx, 2*time.Second)
	defer flushCancel()
	if err := bus.Flush(flushCtx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify all events were processed before shutdown.
	mu.Lock()
	if processedCount != numEvents {
		mu.Unlock()
		t.Fatalf("expected %d events processed after Flush, got %d", numEvents, processedCount)
	}
	mu.Unlock()

	// Stop the listener so workers begin draining.
	listenCancel()

	// Shutdown with ample timeout — workers should drain, <-done fires.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := bus.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Ensure Listen goroutine has fully exited.
	<-listenDone
}

func TestSpinnerInfo(t *testing.T) {
	t.Parallel()

	t.Run("InferenceStartedEvent", func(t *testing.T) {
		tests := []struct {
			name  string
			model string
			want  events.SpinnerInfo
		}{
			{
				name:  "model present",
				model: "gpt-5",
				want:  events.SpinnerInfo{Status: " Thinking [gpt-5]..."},
			},
			{
				name:  "model absent",
				model: "",
				want:  events.SpinnerInfo{Status: " Thinking..."},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := events.InferenceStartedEvent{Model: tt.model}
				got, ok := e.SpinnerInfo()
				if !ok {
					t.Fatal("expected ok=true")
				}
				if got != tt.want {
					t.Errorf("got %+v; want %+v", got, tt.want)
				}
			})
		}
	})

	t.Run("SummarizationStartedEvent", func(t *testing.T) {
		e := events.SummarizationStartedEvent{}
		got, ok := e.SpinnerInfo()
		if !ok {
			t.Fatal("expected ok=true")
		}
		want := events.SpinnerInfo{Status: " Compressing context...", ResetRendering: true}
		if got != want {
			t.Errorf("got %+v; want %+v", got, want)
		}
	})

	t.Run("ToolExecutionStartedEvent", func(t *testing.T) {
		tests := []struct {
			name      string
			toolNames []string
			want      events.SpinnerInfo
		}{
			{
				name:      "no tools",
				toolNames: nil,
				want:      events.SpinnerInfo{Status: " Executing tools...", WithMetrics: true, ResetRendering: true},
			},
			{
				name:      "one tool",
				toolNames: []string{"bash"},
				want:      events.SpinnerInfo{Status: " Executing [bash]...", WithMetrics: true, ResetRendering: true},
			},
			{
				name:      "two tools",
				toolNames: []string{"read", "write"},
				want:      events.SpinnerInfo{Status: " Executing tools [read, write]...", WithMetrics: true, ResetRendering: true},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := events.ToolExecutionStartedEvent{ToolNames: tt.toolNames}
				got, ok := e.SpinnerInfo()
				if !ok {
					t.Fatal("expected ok=true")
				}
				if got != tt.want {
					t.Errorf("got %+v; want %+v", got, tt.want)
				}
			})
		}
	})

	t.Run("RetryWaitingEvent", func(t *testing.T) {
		tests := []struct {
			name     string
			duration time.Duration
			want     events.SpinnerInfo
		}{
			{
				name:     "rounds up to second",
				duration: 1500 * time.Millisecond,
				want:     events.SpinnerInfo{Status: " Retrying in 2s...", ResetRendering: true},
			},
			{
				name:     "sub-second rounds to zero",
				duration: 100 * time.Millisecond,
				want:     events.SpinnerInfo{Status: " Retrying in 0s...", ResetRendering: true},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := events.RetryWaitingEvent{Duration: tt.duration}
				got, ok := e.SpinnerInfo()
				if !ok {
					t.Fatal("expected ok=true")
				}
				if got != tt.want {
					t.Errorf("got %+v; want %+v", got, tt.want)
				}
			})
		}
	})
}
