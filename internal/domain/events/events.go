// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Event represents a generic signal from the Orchestrator.
type Event interface {
	Type() string
}

// Subscriber defines the interface for event handlers.
// CRITICAL: Implementations MUST monitor ctx.Done() and abort
// long-running operations immediately to prevent goroutine leaks.
type Subscriber interface {
	Handle(ctx context.Context, e Event) error
}

var (
	ErrBusClosed         = errors.New("event bus is closed")
	ErrBusNotInitialized = errors.New("event bus is nil or uninitialized")
	errQueueFull         = errors.New("event bus queue is full")
)

const (
	defaultQueueSize         = 1024
	defaultWorkers           = 8
	defaultMaxConcurrentSubs = 1024
)

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(sub func(context.Context, Event))
	Shutdown(ctx context.Context) error
	Flush(ctx context.Context) error
}

// SimpleEventBus is an implementation of EventBus.
type SimpleEventBus struct {
	mu                sync.RWMutex
	subscribers       map[string][]Subscriber
	globalSubscribers []Subscriber
	closed            bool
	closing           chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	log               *slog.Logger

	// Bounded worker pool fields
	eventQueue   chan Event
	numWorkers   int
	subSemaphore chan struct{}
	wg           sync.WaitGroup // Tracks active subscriber dispatches
	workerWG     sync.WaitGroup // Tracks active workers
	pendingWG    sync.WaitGroup // Tracks pending events for Flush
}

// busOption defines a functional option for configuring the SimpleEventBus.
type busOption func(*SimpleEventBus)

// WithLogger sets the logger for the SimpleEventBus.
func WithLogger(l *slog.Logger) busOption {
	return func(b *SimpleEventBus) {
		b.log = l
	}
}

// WithQueueSize sets the size of the internal event queue.
func WithQueueSize(size int) busOption {
	return func(b *SimpleEventBus) {
		b.eventQueue = make(chan Event, size)
	}
}

// WithWorkers sets the number of worker goroutines.
// If n is <= 0, the event bus becomes synchronous (useful for testing).
func WithWorkers(n int) busOption {
	return func(b *SimpleEventBus) {
		b.numWorkers = n
	}
}

// WithMaxConcurrentSubscribers sets the maximum number of concurrent subscriber executions.
func WithMaxConcurrentSubscribers(n int) busOption {
	return func(b *SimpleEventBus) {
		b.subSemaphore = make(chan struct{}, n)
	}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus(ctx context.Context, opts ...busOption) *SimpleEventBus {
	ctx, cancel := context.WithCancel(ctx)
	b := &SimpleEventBus{
		subscribers: make(map[string][]Subscriber),
		closing:     make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		log:         slog.Default(),
		numWorkers:  defaultWorkers,
	}

	for _, opt := range opts {
		opt(b)
	}

	if b.eventQueue == nil {
		b.eventQueue = make(chan Event, defaultQueueSize)
	}

	if b.subSemaphore == nil {
		b.subSemaphore = make(chan struct{}, defaultMaxConcurrentSubs)
	}

	b.startWorkers()

	return b
}

func (b *SimpleEventBus) getLogger() *slog.Logger {
	if b == nil || b.log == nil {
		return slog.Default()
	}
	return b.log
}

func (b *SimpleEventBus) Publish(ctx context.Context, event Event) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	// Always check context first
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return ErrBusClosed
	}

	// Synchronous mode for testing/specific use cases
	if b.numWorkers <= 0 {
		return b.dispatchSync(ctx, event)
	}

	// For async mode, re-acquire RLock to ensure atomicity with Shutdown
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	b.pendingWG.Add(1)
	select {
	case b.eventQueue <- event:
		b.mu.RUnlock()
		return nil
	case <-ctx.Done():
		b.pendingWG.Done()
		b.mu.RUnlock()
		return ctx.Err()
	default:
		b.pendingWG.Done()
		b.mu.RUnlock()
		return errQueueFull
	}
}

func (b *SimpleEventBus) dispatchSync(ctx context.Context, event Event) error {
	b.mu.RLock()
	specificSubs := b.subscribers[event.Type()]
	globalSubs := b.globalSubscribers
	subs := make([]Subscriber, 0, len(specificSubs)+len(globalSubs))
	subs = append(subs, specificSubs...)
	subs = append(subs, globalSubs...)
	b.mu.RUnlock()

	var errs []error
	for _, sub := range subs {
		if err := b.notifySubscriber(ctx, sub, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (b *SimpleEventBus) startWorkers() {
	if b.numWorkers <= 0 {
		return
	}
	b.workerWG.Add(b.numWorkers)
	for i := 0; i < b.numWorkers; i++ {
		go b.workerLoop()
	}
}

func (b *SimpleEventBus) workerLoop() {
	defer b.workerWG.Done()
	for {
		select {
		case <-b.ctx.Done():
			// Drain the queue to decrement pendingWG for unhandled events.
			// Since b.ctx is done, Publish will also start rejecting new events.
			// We just loop until the queue is empty.
			for {
				select {
				case <-b.eventQueue:
					b.pendingWG.Done()
				default:
					return
				}
			}
		case event, ok := <-b.eventQueue:
			if !ok {
				return
			}
			b.dispatch(event)
			b.pendingWG.Done()
		}
	}
}

func (b *SimpleEventBus) dispatch(event Event) {
	b.mu.RLock()
	specificSubs := b.subscribers[event.Type()]
	globalSubs := b.globalSubscribers
	subs := make([]Subscriber, 0, len(specificSubs)+len(globalSubs))
	subs = append(subs, specificSubs...)
	subs = append(subs, globalSubs...)
	b.mu.RUnlock()

	for _, sub := range subs {
		b.wg.Add(1)
		b.pendingWG.Add(1)

		// Acquire semaphore slot (blocks if at max concurrency)
		select {
		case b.subSemaphore <- struct{}{}:
		case <-b.ctx.Done():
			b.wg.Done()
			b.pendingWG.Done()
			return
		}

		go func(s Subscriber, e Event) {
			defer b.wg.Done()
			defer b.pendingWG.Done()
			defer func() { <-b.subSemaphore }()

			// Hard timeout prevents a subscriber from holding the semaphore token forever
			timeoutCtx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
			defer cancel()

			if err := b.notifySubscriber(timeoutCtx, s, e); err != nil {
				b.getLogger().ErrorContext(timeoutCtx, "subscriber failed",
					slog.String("event_type", e.Type()),
					slog.String("subscriber_type", fmt.Sprintf("%T", s)),
					slog.Any("error", err),
				)
			}
		}(sub, event)
	}
}

func (b *SimpleEventBus) notifySubscriber(ctx context.Context, sub Subscriber, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			subType := fmt.Sprintf("%T", sub)
			eventType := event.Type()
			stack := string(debug.Stack())

			// 1. Emit structured log with context
			b.getLogger().ErrorContext(ctx, "Subscriber panicked during event handling",
				slog.String("subscriber_type", subType),
				slog.String("event_type", eventType),
				slog.Any("panic_reason", r),
				slog.String("stack_trace", stack),
			)

			// 2. Format the error to be returned to the caller
			err = fmt.Errorf("subscriber panicked: %v\n%s", r, stack)
		}
	}()

	// Execute the actual subscriber logic here
	return sub.Handle(ctx, event)
}

type funcSubscriber struct {
	f func(context.Context, Event)
}

func (s *funcSubscriber) Handle(ctx context.Context, e Event) error {
	s.f(ctx, e)
	return nil
}

func (b *SimpleEventBus) Subscribe(sub func(context.Context, Event)) {
	if b == nil || sub == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.globalSubscribers = append(b.globalSubscribers, &funcSubscriber{f: sub})
}

// SubscribeGlobal registers a Subscriber that receives all events.
func (b *SimpleEventBus) SubscribeGlobal(sub Subscriber) {
	if b == nil || sub == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.globalSubscribers = append(b.globalSubscribers, sub)
}

// SubscribeSubscriber registers a Subscriber for a specific event type.
func (b *SimpleEventBus) SubscribeSubscriber(eventType string, sub Subscriber) {
	if b == nil || sub == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.subscribers[eventType] = append(b.subscribers[eventType], sub)
}

// Shutdown gracefully stops the event bus.
func (b *SimpleEventBus) Shutdown(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	if b.cancel != nil {
		b.cancel()
	}

	// Drain queue if possible? No, we stop processing to avoid further leaks.

	// Wait for workers and active dispatches to finish
	done := make(chan struct{})
	go func() {
		b.workerWG.Wait()
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Flush waits for all currently queued events to be processed or context timeout.
func (b *SimpleEventBus) Flush(ctx context.Context) error {
	if b == nil {
		return ErrBusNotInitialized
	}
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return ErrBusClosed
	}

	done := make(chan struct{})
	go func() {
		b.pendingWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return ErrBusClosed
	}
}

// StatusUpdate signals a change in the agent's internal state or progress.
type StatusUpdate struct {
	Message string
	Level   string
}

// TurnStarted signals the beginning of a new Think-Act-Observe cycle.
type TurnStarted struct {
	Turn     int
	MaxTurns int
}

// InferenceStartedEvent signals that the agent is starting to generate a response.
type InferenceStartedEvent struct{}

// RefiningStartedEvent signals that the agent is starting to refine or recover context.
type RefiningStartedEvent struct{}

// SummarizationStartedEvent signals that the history summarization process has begun.
type SummarizationStartedEvent struct{}

// ResponseEvent carries the final LLM output.
type ResponseEvent struct {
	Content *llm.Content
}

// ToolCallEvent signals that one or more tools are being invoked.
type ToolCallEvent struct {
	Calls    []*llm.FunctionCall
	Turn     int
	MaxTurns int
}

// ToolResultEvent signals that a tool has finished execution.
type ToolResultEvent struct {
	Name   string
	Result tools.ToolResult
}

// UsageMetricsEvent signals that a turn is complete and usage should be recorded.
type UsageMetricsEvent struct {
	Context   context.Context
	Metrics   *llm.Metrics
	LogFile   string
	StartTime time.Time
}

// SystemMessageEvent signals a system-level message (error, warning, info).
type SystemMessageEvent struct {
	Message string
	Level   string
}

// TokenLimitReachedEvent signals that the conversation has reached its token limit.
type TokenLimitReachedEvent struct {
	Tokens   int
	MaxLimit int
}

// SummarizationRequired signals that the history is becoming too large and should be summarized.
type SummarizationRequired struct {
	Tokens   int
	MaxLimit int
	Reason   string
}

// TraceEvent carries the TurnTrace for a completed turn.
type TraceEvent struct {
	Trace *telemetry.TurnTrace
}

// SafePublish attempts to publish an event with a forced timeout.
// It returns an error if the context is cancelled, the queue is full, or the publication fails.
func SafePublish(ctx context.Context, bus EventBus, event Event) error {
	if bus == nil {
		return ErrBusNotInitialized
	}

	// Now bus.Publish is asynchronous and non-blocking (up to queue size).
	// We still apply a strict 2s limit to prevent the publisher from hanging if the queue is full.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err := bus.Publish(ctx, event)
	if err == nil {
		return nil
	}

	// Handle publish failures (mostly context timeout/cancellation)
	wrappedErr := fmt.Errorf("publish failure for event %s: %w", event.Type(), err)

	logger := slog.Default()
	if l, ok := bus.(interface{ getLogger() *slog.Logger }); ok {
		logger = l.getLogger()
	}

	logger.WarnContext(ctx, "Event dropped due to publish failure",
		slog.String("event_type", event.Type()),
		slog.String("error", wrappedErr.Error()),
	)
	return wrappedErr
}

func (e StatusUpdate) Type() string              { return "StatusUpdate" }
func (e TurnStarted) Type() string               { return "TurnStarted" }
func (e InferenceStartedEvent) Type() string     { return "InferenceStartedEvent" }
func (e ResponseEvent) Type() string             { return "ResponseEvent" }
func (e ToolCallEvent) Type() string             { return "ToolCallEvent" }
func (e ToolResultEvent) Type() string           { return "ToolResultEvent" }
func (e UsageMetricsEvent) Type() string         { return "UsageMetricsEvent" }
func (e SystemMessageEvent) Type() string        { return "SystemMessageEvent" }
func (e TokenLimitReachedEvent) Type() string    { return "TokenLimitReachedEvent" }
func (e SummarizationRequired) Type() string     { return "SummarizationRequired" }
func (e TraceEvent) Type() string                { return "TraceEvent" }
func (e RefiningStartedEvent) Type() string      { return "RefiningStartedEvent" }
func (e SummarizationStartedEvent) Type() string { return "SummarizationStartedEvent" }
