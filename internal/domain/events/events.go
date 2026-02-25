// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Event represents a generic signal from the Orchestrator.
type Event interface{}

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(e Event)
	Subscribe(sub func(Event))
	Shutdown(ctx context.Context) error
	Flush(ctx context.Context) error
}

// DefaultMaxQueueSize is the default capacity for the event ring buffer.
const DefaultMaxQueueSize = 1000

// SimpleEventBus is an asynchronous implementation of EventBus that uses a buffered channel.
type SimpleEventBus struct {
	mu          sync.RWMutex
	subscribers []chan Event
	wg          sync.WaitGroup
	once        sync.Once
	closed      bool
	capacity    int
	closing     chan struct{}
}

type flushEvent struct {
	done chan struct{}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus with the default capacity.
func NewSimpleEventBus() *SimpleEventBus {
	return NewSimpleEventBusWithCapacity(DefaultMaxQueueSize)
}

// NewSimpleEventBusWithCapacity creates a new SimpleEventBus with a custom ring buffer capacity.
func NewSimpleEventBusWithCapacity(capacity int) *SimpleEventBus {
	if capacity <= 0 {
		capacity = DefaultMaxQueueSize
	}
	return &SimpleEventBus{
		capacity: capacity,
		closing:  make(chan struct{}),
	}
}

func (b *SimpleEventBus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, ch := range b.subscribers {
		select {
		case ch <- e:
		default:
			// Buffer full, drop event to avoid deadlocking the RLock
		}
	}
}

func (b *SimpleEventBus) Subscribe(sub func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	in := make(chan Event, 100) // Small initial buffer
	out := make(chan Event)
	b.subscribers = append(b.subscribers, in)

	b.wg.Add(2)

	// 1. Bounded Queue Goroutine
	go func() {
		defer b.wg.Done()
		defer close(out)
		b.pumpEvents(in, out)
	}()

	// 2. Processing Goroutine
	go func() {
		defer b.wg.Done()
		for e := range out {
			if fe, ok := e.(flushEvent); ok {
				close(fe.done)
				continue
			}
			sub(e)
		}
	}()
}

func (b *SimpleEventBus) pumpEvents(in chan Event, out chan<- Event) {
	cap := b.capacity
	if cap <= 0 {
		cap = DefaultMaxQueueSize
	}
	buffer := &eventRingBuffer{max: cap}

	for {
		if buffer.len() > 0 {
			select {
			case e, ok := <-in:
				if !ok {
					in = nil // Stop reading from closed channel
					continue
				}
				buffer.push(e)
			case out <- buffer.front():
				buffer.pop()
			}
		} else {
			if in == nil {
				return
			}
			e, ok := <-in
			if !ok {
				return
			}
			buffer.push(e)
		}
	}
}

type eventRingBuffer struct {
	queue []Event
	max   int
	head  int
	tail  int
	count int
}

func (r *eventRingBuffer) push(e Event) {
	if r.queue == nil {
		r.queue = make([]Event, r.max)
	}

	if r.count == r.max {
		// Buffer full: overwrite the oldest element
		oldest := r.queue[r.tail]
		if fe, ok := oldest.(flushEvent); ok {
			close(fe.done) // Safely unblock the waiting Flush caller
		}

		r.queue[r.tail] = e
		r.tail = (r.tail + 1) % r.max
		r.head = (r.head + 1) % r.max // Move head forward to evict
	} else {
		r.queue[r.tail] = e
		r.tail = (r.tail + 1) % r.max
		r.count++
	}
}

func (r *eventRingBuffer) pop() Event {
	if r.count == 0 {
		return nil
	}
	e := r.queue[r.head]
	r.queue[r.head] = nil // Avoid memory leak
	r.head = (r.head + 1) % r.max
	r.count--
	return e
}

func (r *eventRingBuffer) front() Event {
	if r.count == 0 {
		return nil
	}
	return r.queue[r.head]
}

func (r *eventRingBuffer) len() int {
	return r.count
}

// Shutdown gracefully stops the event bus, flushing pending events.
func (b *SimpleEventBus) Shutdown(ctx context.Context) error {
	b.once.Do(func() {
		b.mu.Lock()
		b.closed = true
		close(b.closing)
		for _, ch := range b.subscribers {
			close(ch)
		}
		b.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
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

// Flush waits for all currently queued events to be dispatched.
func (b *SimpleEventBus) Flush(ctx context.Context) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("event bus is closed")
	}

	if len(b.subscribers) == 0 {
		b.mu.RUnlock()
		return nil
	}

	// 1. Copy subscribers to avoid holding the lock during potentially blocking sends
	subs := make([]chan Event, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock() // Release lock BEFORE the blocking loop

	dones := make([]chan struct{}, 0, len(subs))

	// 2. Safely send flush events to the copied channels
	for _, ch := range subs {
		done := make(chan struct{})
		
		select {
		case ch <- flushEvent{done: done}:
			dones = append(dones, done)
		case <-b.closing:
			return fmt.Errorf("event bus is closed")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// 3. Wait for all successfully queued flush events to be processed
	for _, done := range dones {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
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
