// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Limits defines the operational constraints for the agent.
type Limits struct {
	MaxHistoryTokens int
	MaxToolTurns     int
	MaxHistoryTurns  int
	TieredThreshold  int
	ContextWindow    int
}

// ConfigUpdated signals that the agent's configuration has changed.
type ConfigUpdated struct {
	Limits Limits
}

// TurnStatus contains the data needed for rendering turn status.
type TurnStatus struct {
	Timestamp        time.Time
	CurrentTurns     int
	SessionTurns     int // New: Total turns in history
	MaxHistoryTurns  int
	Tokens           int
	MaxHistoryTokens int
	TieredThreshold  int
	Metrics          *llm.Metrics
	IsPostCall       bool
	IsFinal          bool // True if the agent is stopping and yielding control to user
	StartTime        time.Time
	SessionCost      float64
	DailyCost        float64
	TaskCost         float64
	TotalM           int64
	TotalH           int64
	TotalO           int64
	ToolReasons      []string // Captures tool reasons to print at the end
	Mode             string
}

// TurnStatusEvent carries payload and token metrics for UI display.
type TurnStatusEvent struct {
	Status TurnStatus
}

func (e ConfigUpdated) Type() string   { return "ConfigUpdated" }
func (e TurnStatusEvent) Type() string { return "TurnStatusEvent" }

// StatusUpdate signals a change in the agent's internal state or progress.
type StatusUpdate struct {
	Message string
	Level   string
}

func (e StatusUpdate) Type() string { return "StatusUpdate" }

// TurnStarted signals the beginning of a new Think-Act-Observe cycle.
type TurnStarted struct {
	Turn     int
	MaxTurns int
}

func (e TurnStarted) Type() string { return "TurnStarted" }

// InferenceStartedEvent signals that the agent is starting to generate a response.
type InferenceStartedEvent struct {
	Model string
}

func (e InferenceStartedEvent) Type() string { return "InferenceStartedEvent" }

// SummarizationStartedEvent signals that the history summarization process has begun.
type SummarizationStartedEvent struct{}

func (e SummarizationStartedEvent) Type() string { return "SummarizationStartedEvent" }

// ResponseEvent carries the final LLM output.
type ResponseEvent struct {
	Content *llm.Content
}

func (e ResponseEvent) Type() string { return "ResponseEvent" }

// ToolCallEvent signals that one or more tools are being invoked.
type ToolCallEvent struct {
	Calls    []*llm.FunctionCall
	Turn     int
	MaxTurns int
}

func (e ToolCallEvent) Type() string { return "ToolCallEvent" }

// ToolExecutionStartedEvent signals that the tool execution phase has started.
type ToolExecutionStartedEvent struct {
	ToolNames []string
}

func (e ToolExecutionStartedEvent) Type() string { return "ToolExecutionStartedEvent" }

// ToolResultEvent signals that a tool has finished execution.
type ToolResultEvent struct {
	Name   string
	Result tools.ToolResult
}

func (e ToolResultEvent) Type() string { return "ToolResultEvent" }

// UsageMetricsEvent signals that a turn is complete and usage should be recorded.
type UsageMetricsEvent struct {
	Context   context.Context
	Metrics   *llm.Metrics
	LogFile   string
	StartTime time.Time
}

func (e UsageMetricsEvent) Type() string { return "UsageMetricsEvent" }

// SystemMessageEvent signals a system-level message (error, warning, info).
type SystemMessageEvent struct {
	Message string
	Level   string
}

func (e SystemMessageEvent) Type() string { return "SystemMessageEvent" }

// TokenLimitReachedEvent signals that the conversation has reached its token limit.
type TokenLimitReachedEvent struct {
	Tokens   int
	MaxLimit int
}

func (e TokenLimitReachedEvent) Type() string { return "TokenLimitReachedEvent" }

// SummarizationRequired signals that the history is becoming too large and should be summarized.
type SummarizationRequired struct {
	Tokens   int
	MaxLimit int
	Reason   string
}

func (e SummarizationRequired) Type() string { return "SummarizationRequired" }

// TraceEvent carries the TurnTrace for a completed turn.
type TraceEvent struct {
	Trace *telemetry.TurnTrace
}

func (e TraceEvent) Type() string { return "TraceEvent" }

// RetryWaitingEvent signals that the agent is waiting before retrying a failed operation.
type RetryWaitingEvent struct {
	Duration time.Duration
}

func (e RetryWaitingEvent) Type() string { return "RetryWaitingEvent" }

// ConsentStartedEvent signals that the user is being prompted for tool consent.
type ConsentStartedEvent struct{}

func (e ConsentStartedEvent) Type() string { return "ConsentStartedEvent" }

// ConsentFinishedEvent signals that the user consent prompt has finished.
type ConsentFinishedEvent struct{}

func (e ConsentFinishedEvent) Type() string { return "ConsentFinishedEvent" }
