// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Event represents a generic signal from the Orchestrator.
type Event interface{}

// EventBus defines the interface for publishing and subscribing to events.
type EventBus interface {
	Publish(e Event)
	Subscribe(sub func(Event))
}

// SimpleEventBus is a basic implementation of EventBus.
type SimpleEventBus struct {
	mu          sync.RWMutex
	subscribers []func(Event)
}

func (b *SimpleEventBus) Publish(e Event) {
	b.mu.RLock()
	subs := make([]func(Event), len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock()

	for _, sub := range subs {
		sub(e)
	}
}

func (b *SimpleEventBus) Subscribe(sub func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, sub)
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
