// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"time"

	"github.com/gosharplite/tell-me-go/internal/types"
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
	subscribers []func(Event)
}

func (b *SimpleEventBus) Publish(e Event) {
	for _, sub := range b.subscribers {
		sub(e)
	}
}

func (b *SimpleEventBus) Subscribe(sub func(Event)) {
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

// TurnStatusEvent carries payload and token metrics for UI display.
type TurnStatusEvent struct {
	Status TurnStatus
}

// ResponseStreamEvent carries a channel for streaming LLM output.
type ResponseStreamEvent struct {
	Context context.Context
	Stream  <-chan *types.Content
}

// ToolCallEvent signals that one or more tools are being invoked.
type ToolCallEvent struct {
	Calls    []*types.FunctionCall
	Turn     int
	MaxTurns int
}

// ToolResultEvent signals that a tool has finished execution.
type ToolResultEvent struct {
	Name   string
	Result types.ToolResult
}

// UsageMetricsEvent signals that a turn is complete and usage should be recorded.
type UsageMetricsEvent struct {
	Metrics   *types.Metrics
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
