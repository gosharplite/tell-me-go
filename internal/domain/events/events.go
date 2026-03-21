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
type Subscriber interface {
	Handle(ctx context.Context, e Event) error
}

var (
	ErrBusClosed         = errors.New("event bus is closed")
	errBusNotInitialized = errors.New("event bus is nil or uninitialized")
)

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(sub func(Event))
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
	logger            *slog.Logger
}

// BusOption defines a functional option for configuring the SimpleEventBus.
type BusOption func(*SimpleEventBus)

// WithLogger sets the logger for the SimpleEventBus.
func WithLogger(l *slog.Logger) BusOption {
	return func(b *SimpleEventBus) {
		b.logger = l
	}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus(ctx context.Context, opts ...BusOption) *SimpleEventBus {
	ctx, cancel := context.WithCancel(ctx)
	b := &SimpleEventBus{
		subscribers: make(map[string][]Subscriber),
		closing:     make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		logger:      slog.Default(),
	}

	for _, opt := range opts {
		opt(b)
	}

	return b
}

func (b *SimpleEventBus) Logger() *slog.Logger {
	if b == nil || b.logger == nil {
		return slog.Default()
	}
	return b.logger
}

func (b *SimpleEventBus) Publish(ctx context.Context, event Event) error {
	if b == nil {
		return errBusNotInitialized
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	// 1. Safely copy the subscriber slices while holding the read lock.
	// This prevents data races when subscribers are added/removed during iteration,
	// and avoids deadlocks if a subscriber tries to publish/subscribe recursively.
	specificSubs := b.subscribers[event.Type()]
	globalSubs := b.globalSubscribers

	subs := make([]Subscriber, 0, len(specificSubs)+len(globalSubs))
	subs = append(subs, specificSubs...)
	subs = append(subs, globalSubs...)
	b.mu.RUnlock()

	// 2. Iterate over the local copy outside the lock.
	var errs []error
	for _, sub := range subs {
		if err := b.notifySubscriber(ctx, sub, event); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (b *SimpleEventBus) notifySubscriber(ctx context.Context, sub Subscriber, event Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			subType := fmt.Sprintf("%T", sub)
			eventType := event.Type()
			stack := string(debug.Stack())

			// 1. Emit structured log with context
			b.Logger().ErrorContext(ctx, "Subscriber panicked during event handling",
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
	f func(Event)
}

func (s *funcSubscriber) Handle(ctx context.Context, e Event) error {
	s.f(e)
	return nil
}

func (b *SimpleEventBus) Subscribe(sub func(Event)) {
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
		return errBusNotInitialized
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.cancel != nil {
		b.cancel()
	}
	return nil
}

// Flush is a no-op as this implementation is synchronous.
func (b *SimpleEventBus) Flush(ctx context.Context) error {
	if b == nil {
		return errBusNotInitialized
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBusClosed
	}
	return nil
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

// ResponseStreamEvent carries a channel for streaming LLM output.
type ResponseStreamEvent struct {
	Context context.Context
	Stream  <-chan *llm.Content
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
// It returns an error if the context is cancelled or the publication fails (e.g., buffer overflow).
func SafePublish(ctx context.Context, bus EventBus, event Event) error {
	if bus == nil {
		return errBusNotInitialized
	}

	// Enforce a strict timeout limit internally to prevent deadlocks from stalled subscribers
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	ch := make(chan error, 1) // Buffered to prevent leak
	go func() {
		ch <- bus.Publish(ctx, event)
	}()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		err := fmt.Errorf("publish timeout for event %s: %w", event.Type(), ctx.Err())

		logger := slog.Default()
		if l, ok := bus.(interface{ Logger() *slog.Logger }); ok {
			logger = l.Logger()
		}

		// 1. Emit structured log with context ensuring visibility even if caller drops the error
		logger.WarnContext(ctx, "Event dropped due to publish timeout",
			slog.String("event_type", event.Type()),
			slog.String("error", err.Error()),
		)

		return err
	}
}

func (e StatusUpdate) Type() string { return "StatusUpdate" }
func (e TurnStarted) Type() string { return "TurnStarted" }
func (e ResponseStreamEvent) Type() string { return "ResponseStreamEvent" }
func (e ToolCallEvent) Type() string { return "ToolCallEvent" }
func (e ToolResultEvent) Type() string { return "ToolResultEvent" }
func (e UsageMetricsEvent) Type() string { return "UsageMetricsEvent" }
func (e SystemMessageEvent) Type() string { return "SystemMessageEvent" }
func (e TokenLimitReachedEvent) Type() string { return "TokenLimitReachedEvent" }
func (e SummarizationRequired) Type() string { return "SummarizationRequired" }
func (e TraceEvent) Type() string { return "TraceEvent" }
