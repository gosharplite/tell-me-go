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

	"golang.org/x/sync/errgroup"
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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
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
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(false))
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
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(false))
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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithLogger(nullLogger), events.WithAsync(true))
	inframock.CleanupBus(t, bus)

	var wg sync.WaitGroup

	// Coordinated background listener
	// We don't add this to wg because we want it to run until we cancel the context
	// after all other tasks are done.
	go func() {
		_ = bus.Listen(ctx)
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
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
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
	g, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	bus := events.NewSimpleEventBus(ctx, events.WithLogger(testLogger), events.WithAsync(true))
	inframock.CleanupBus(t, bus)

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
	}

	for _, e := range events_list {
		if e.Type() == "" {
			t.Errorf("empty type for %T", e)
		}
	}
}

func TestSafePublish_NoGoroutineLeak(t *testing.T) {
	t.Parallel()
	g, ctx := errgroup.WithContext(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	bus := events.NewSimpleEventBus(ctx, events.WithAsync(true))
	inframock.CleanupBus(t, bus)

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
	inframock.CleanupBus(t, bus)

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
	block             chan struct{}
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

	buf := inframock.NewSafeBuffer()
	testLogger := slog.New(slog.NewJSONHandler(buf, nil))

	// Setup bus with tiny queue to force backpressure quickly
	bus := events.NewSimpleEventBus(ctx,
		events.WithLogger(testLogger),
		events.WithAsync(true),
		events.WithQueueSize(1),
	)
	inframock.CleanupBus(t, bus)

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

	if duration > 100*time.Millisecond {
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
		inframock.CleanupBus(t, bus)

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
