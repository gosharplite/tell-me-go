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

// SimpleEventBus is an asynchronous implementation of EventBus that uses a buffered channel.
type SimpleEventBus struct {
	mu          sync.RWMutex
	subscribers []chan Event
	wg          sync.WaitGroup
	once        sync.Once
	closed      bool
}

type flushEvent struct {
	done chan struct{}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus() *SimpleEventBus {
	return &SimpleEventBus{}
}

func (b *SimpleEventBus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, ch := range b.subscribers {
		ch <- e
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
	const maxQueueSize = 1000
	buffer := &eventRingBuffer{max: maxQueueSize}

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
}

func (r *eventRingBuffer) push(e Event) {
	if len(r.queue) >= r.max {
		r.queue[0] = nil // Avoid memory leak
		r.queue = r.queue[1:]
	}
	r.queue = append(r.queue, e)
}

func (r *eventRingBuffer) pop() Event {
	e := r.queue[0]
	r.queue[0] = nil // Avoid memory leak
	r.queue = r.queue[1:]
	return e
}

func (r *eventRingBuffer) front() Event {
	return r.queue[0]
}

func (r *eventRingBuffer) len() int {
	return len(r.queue)
}

// Shutdown gracefully stops the event bus, flushing pending events.
func (b *SimpleEventBus) Shutdown(ctx context.Context) error {
	b.once.Do(func() {
		b.mu.Lock()
		b.closed = true
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

	dones := make([]chan struct{}, 0, len(b.subscribers))

	// Hold RLock while sending to prevent Shutdown from closing channels under us
	for _, ch := range b.subscribers {
		done := make(chan struct{})
		select {
		case ch <- flushEvent{done: done}:
			dones = append(dones, done)
		case <-ctx.Done():
			b.mu.RUnlock()
			return ctx.Err()
		}
	}
	b.mu.RUnlock() // Release lock BEFORE waiting for subscribers to process the events

	// Wait for all queued events to be processed
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
