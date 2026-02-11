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
	subscribers []func(Event)
	queue       chan Event
	stop        chan struct{}
	wg          sync.WaitGroup
	once        sync.Once
}

type flushEvent struct {
	done chan struct{}
}

// NewSimpleEventBus creates and initializes a new SimpleEventBus.
func NewSimpleEventBus() *SimpleEventBus {
	b := &SimpleEventBus{
		queue: make(chan Event, 100),
		stop:  make(chan struct{}),
	}
	b.wg.Add(1)
	go b.worker()
	return b
}

func (b *SimpleEventBus) worker() {
	defer b.wg.Done()
	for {
		select {
		case e, ok := <-b.queue:
			if !ok {
				return
			}
			b.dispatch(e)
		case <-b.stop:
			// Flush remaining events
			for {
				select {
				case e, ok := <-b.queue:
					if ok {
						b.dispatch(e)
					} else {
						return
					}
				default:
					return
				}
			}
		}
	}
}

func (b *SimpleEventBus) dispatch(e Event) {
	if fe, ok := e.(flushEvent); ok {
		close(fe.done)
		return
	}

	b.mu.RLock()
	subs := make([]func(Event), len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock()

	for _, sub := range subs {
		sub(e)
	}
}

func (b *SimpleEventBus) Publish(e Event) {
	if b.queue == nil {
		b.dispatch(e)
		return
	}

	select {
	case b.queue <- e:
	case <-b.stop:
		// Bus is shutting down, ignore
	default:
		// Buffer full, drop event to avoid blocking the caller
	}
}

func (b *SimpleEventBus) Subscribe(sub func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, sub)
}

// Shutdown gracefully stops the event bus, flushing pending events.
func (b *SimpleEventBus) Shutdown(ctx context.Context) error {
	if b.queue == nil {
		return nil
	}

	b.once.Do(func() {
		close(b.stop)
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
	if b.queue == nil {
		return nil
	}

	done := make(chan struct{})
	select {
	case b.queue <- flushEvent{done: done}:
	case <-b.stop:
		return fmt.Errorf("event bus is closed")
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
