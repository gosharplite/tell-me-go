// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Event represents a generic signal from the Orchestrator.
type Event interface{}

var (
	ErrBufferOverflow = errors.New("event buffer overflowed, events were dropped")
	ErrBusClosed      = errors.New("event bus is closed")
)

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(ctx context.Context, e Event) error
	Subscribe(sub func(Event))
	Shutdown(ctx context.Context) error
	Flush(ctx context.Context) error
}

// defaultMaxQueueSize is the default capacity for the event ring buffer.
const defaultMaxQueueSize = 1000

// SimpleEventBus is an asynchronous implementation of EventBus that uses a buffered channel.
type SimpleEventBus struct {
	mu              sync.RWMutex
	subscribers     []chan Event
	wg              sync.WaitGroup
	once            sync.Once
	closed          bool
	capacity        int
	closing         chan struct{}
	activeProducers sync.WaitGroup // Tracks lockless senders like Flush
	droppedEvents   uint64         // Tracks events dropped at the channel level
	ctx             context.Context
	cancel          context.CancelFunc
}

// DroppedEvents returns the number of events dropped due to buffer capacity.
func (b *SimpleEventBus) DroppedEvents() uint64 {
	return atomic.LoadUint64(&b.droppedEvents)
}

type flushEvent struct {
	done chan error
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus with the default capacity.
func NewSimpleEventBus() *SimpleEventBus {
	return NewSimpleEventBusWithCapacity(defaultMaxQueueSize)
}

// NewSimpleEventBusWithCapacity creates a new SimpleEventBus with a custom ring buffer capacity.
func NewSimpleEventBusWithCapacity(capacity int) *SimpleEventBus {
	if capacity <= 0 {
		capacity = defaultMaxQueueSize
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &SimpleEventBus{
		capacity: capacity,
		closing:  make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (b *SimpleEventBus) Publish(ctx context.Context, e Event) error {
	// 1. Immediate context check
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return ErrBusClosed
	}

	for _, ch := range b.subscribers {
		select {
		case ch <- e:
		case <-ctx.Done():
			return ctx.Err()
		case <-b.closing:
			return ErrBusClosed
		default:
			// SCALABLE: Drop event if subscriber is too slow/stuck to prevent head-of-line blocking
			atomic.AddUint64(&b.droppedEvents, 1)
		}
	}
	return nil
}

func (b *SimpleEventBus) Subscribe(sub func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	cap := b.getEffectiveCapacity()
	in := make(chan Event, cap)
	out := make(chan Event)
	b.subscribers = append(b.subscribers, in)

	b.wg.Add(2)
	ctx := b.getSafeContext()

	go b.startEventPump(ctx, in, out)
	go b.startSubscriberLoop(ctx, out, sub)
}

func (b *SimpleEventBus) getEffectiveCapacity() int {
	if b.capacity <= 0 {
		return defaultMaxQueueSize
	}
	return b.capacity
}

func (b *SimpleEventBus) getSafeContext() context.Context {
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *SimpleEventBus) startEventPump(ctx context.Context, in chan Event, out chan Event) {
	defer b.wg.Done()
	defer close(out)
	b.pumpEvents(ctx, in, out)
}

func (b *SimpleEventBus) startSubscriberLoop(ctx context.Context, out chan Event, sub func(Event)) {
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-out:
			if !ok {
				return
			}
			if b.handleInternalEvent(ctx, e) {
				continue
			}
			sub(e)
		}
	}
}

func (b *SimpleEventBus) handleInternalEvent(ctx context.Context, e Event) bool {
	if fe, ok := e.(flushEvent); ok {
		select {
		case fe.done <- nil:
		case <-ctx.Done():
		}
		close(fe.done)
		return true
	}
	return false
}

func (b *SimpleEventBus) pumpEvents(ctx context.Context, in chan Event, out chan<- Event) {
	cap := b.capacity
	if cap <= 0 {
		cap = defaultMaxQueueSize
	}
	buffer := &eventRingBuffer{max: cap}

	for {
		if buffer.len() > 0 {
			if stop := b.processWithBuffer(ctx, &in, out, buffer); stop {
				return
			}
		} else {
			if in == nil {
				return
			}
			if stop := b.processEmptyBuffer(ctx, &in, buffer); stop {
				return
			}
		}
	}
}

func (b *SimpleEventBus) processWithBuffer(ctx context.Context, in *chan Event, out chan<- Event, buffer *eventRingBuffer) bool {
	select {
	case <-ctx.Done():
		b.cleanupBuffer(ctx, buffer, ctx.Err())
		return true
	case e, ok := <-*in:
		if !ok {
			*in = nil // Stop reading from closed channel
			return false
		}
		buffer.push(ctx, e)
		return false
	case out <- buffer.front():
		buffer.pop()
		return false
	}
}

func (b *SimpleEventBus) processEmptyBuffer(ctx context.Context, in *chan Event, buffer *eventRingBuffer) bool {
	select {
	case <-ctx.Done():
		return true
	case e, ok := <-*in:
		if !ok {
			return true
		}
		buffer.push(ctx, e)
		return false
	}
}

func (b *SimpleEventBus) cleanupBuffer(ctx context.Context, buffer *eventRingBuffer, err error) {
	for buffer.len() > 0 {
		e := buffer.pop()
		if fe, ok := e.(flushEvent); ok {
			select {
			case fe.done <- err:
			case <-ctx.Done():
			}
			close(fe.done)
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

func (r *eventRingBuffer) push(ctx context.Context, e Event) {
	if r.queue == nil {
		r.queue = make([]Event, r.max)
	}

	if r.count == r.max {
		// Buffer full: overwrite the oldest element
		oldest := r.queue[r.tail]
		if fe, ok := oldest.(flushEvent); ok {
			select {
			case fe.done <- ErrBufferOverflow:
			case <-ctx.Done():
			}
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
		// 1. Mark as closed and signal pending producers (Flush) to abort
		b.mu.Lock()
		b.closed = true
		if b.closing != nil {
			close(b.closing)
		}
		b.mu.Unlock()

		// 2. Wait for everything to finish naturally in a background goroutine
		done := make(chan struct{})
		go func() {
			// a. Wait for active producers (Flush) to acknowledge the closing signal and exit
			b.activeProducers.Wait()

			// b. Close all subscriber input channels to signal pumpEvents to drain
			b.mu.Lock()
			subs := b.subscribers
			b.subscribers = nil
			b.mu.Unlock()

			for _, ch := range subs {
				close(ch)
			}

			// c. Wait for background processing (pumpEvents and subscriber callbacks) to stop
			b.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Graceful shutdown complete
		case <-ctx.Done():
			// Timeout! Force termination by canceling internal context.
			b.abort()
		}
	})
	return ctx.Err()
}

func (b *SimpleEventBus) abort() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

// Flush waits for all currently queued events to be dispatched.
func (b *SimpleEventBus) Flush(ctx context.Context) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	// Register active producer before releasing lock
	b.activeProducers.Add(1)
	defer b.activeProducers.Done()

	if len(b.subscribers) == 0 {
		b.mu.RUnlock()
		return nil
	}

	// 1. Copy subscribers to avoid holding the lock during potentially blocking sends
	subs := make([]chan Event, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock() // Release lock BEFORE the blocking loop

	var wg sync.WaitGroup
	errCh := make(chan error, len(subs))

	for _, ch := range subs {
		wg.Add(1)
		go func(subCh chan Event) {
			defer wg.Done()
			if err := b.flushSubscriber(ctx, subCh); err != nil {
				errCh <- err
			}
		}(ch)
	}

	// Launch a background goroutine to wait and close the error channel
	go func() {
		wg.Wait()
		close(errCh)
	}()

	// Finally, in the main Flush thread, wait for all results
	var errs []error
	for err := range errCh {
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (b *SimpleEventBus) flushSubscriber(ctx context.Context, subCh chan Event) error {
	done := make(chan error, 1)
	select {
	case subCh <- flushEvent{done: done}:
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-b.closing:
			return ErrBusClosed
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-b.closing:
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
