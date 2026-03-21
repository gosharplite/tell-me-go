// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
	"fmt"
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
	ErrBufferOverflow    = errors.New("event buffer overflowed, events were dropped")
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
	mu          sync.RWMutex
	subscribers map[string][]Subscriber
	closed      bool
	closing     chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus(ctx context.Context) *SimpleEventBus {
	ctx, cancel := context.WithCancel(ctx)
	return &SimpleEventBus{
		subscribers: make(map[string][]Subscriber),
		closing:     make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}
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
	// Copy the slice to avoid race conditions after releasing the lock
	subs := append([]Subscriber(nil), b.subscribers[event.Type()]...)
	b.mu.RUnlock() // Release lock early to prevent deadlocks

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
			err = fmt.Errorf("subscriber panicked: %v", r)
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

var allKnownTypes = []string{
	"StatusUpdate",
	"TurnStarted",
	"ResponseStreamEvent",
	"ToolCallEvent",
	"ToolResultEvent",
	"UsageMetricsEvent",
	"SystemMessageEvent",
	"TokenLimitReachedEvent",
	"SummarizationRequired",
	"TraceEvent",
	"ConfigUpdated",
	"TurnStatusEvent",
	"testEvent",
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

	s := &funcSubscriber{f: sub}
	for _, t := range allKnownTypes {
		b.subscribers[t] = append(b.subscribers[t], s)
	}
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
func SafePublish(ctx context.Context, bus EventBus, e Event, timeout time.Duration) error {
	if bus == nil {
		return errBusNotInitialized
	}
	pubCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return bus.Publish(pubCtx, e)
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
