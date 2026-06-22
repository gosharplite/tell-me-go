// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"fmt"
	"strings"
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
	ContextWindow    int
}

// Validate checks that the Limits contain non-negative values required by the
// session context Manager. It is called by Manager.Reconfigure to fail fast on
// malformed limits. See ADR-029.
//
// Rules (minimum viable set per ADR-029 §6):
//   - MaxHistoryTokens must be >= 0.
//   - MaxToolTurns must be >= 0.
//   - MaxHistoryTurns must be >= 0.
//
// A value of zero is treated as "use default / unlimited" by downstream
// consumers and is therefore valid here. Negative values are nonsensical and
// indicate misconfiguration.
//
// Additional validation rules will be added in separate, individually-tracked
// issues per ADR-029 negative-consequence mitigation. Do not extend this method
// ad-hoc.
func (l Limits) Validate() error {
	if l.MaxHistoryTokens < 0 {
		return fmt.Errorf("limits: max history tokens must be >= 0, got %d", l.MaxHistoryTokens)
	}
	if l.MaxToolTurns < 0 {
		return fmt.Errorf("limits: max tool turns must be >= 0, got %d", l.MaxToolTurns)
	}
	if l.MaxHistoryTurns < 0 {
		return fmt.Errorf("limits: max history turns must be >= 0, got %d", l.MaxHistoryTurns)
	}
	return nil
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

// SpinnerInfo returns the spinner presentation for an inference-started event.
// When Model is set, the model name is included in the status text.
func (e InferenceStartedEvent) SpinnerInfo() (SpinnerInfo, bool) {
	status := " Thinking..."
	if e.Model != "" {
		status = fmt.Sprintf(" Thinking [%s]...", e.Model)
	}
	return SpinnerInfo{Status: status}, true
}

// SummarizationStartedEvent signals that the history summarization process has begun.
type SummarizationStartedEvent struct{}

func (e SummarizationStartedEvent) Type() string { return "SummarizationStartedEvent" }

// SpinnerInfo returns the spinner presentation for a summarization-started event.
func (e SummarizationStartedEvent) SpinnerInfo() (SpinnerInfo, bool) {
	return SpinnerInfo{Status: " Compressing context...", ResetRendering: true}, true
}

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

// SpinnerInfo returns the spinner presentation for a tool-execution-started event.
// The status text includes tool names when available.
func (e ToolExecutionStartedEvent) SpinnerInfo() (SpinnerInfo, bool) {
	status := " Executing tools..."
	if len(e.ToolNames) == 1 {
		status = fmt.Sprintf(" Executing [%s]...", e.ToolNames[0])
	} else if len(e.ToolNames) > 1 {
		status = fmt.Sprintf(" Executing tools [%s]...", strings.Join(e.ToolNames, ", "))
	}
	return SpinnerInfo{Status: status, WithMetrics: true, ResetRendering: true}, true
}

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

// SpinnerInfo returns the spinner presentation for a retry-waiting event.
// The status text includes the rounded wait duration.
func (e RetryWaitingEvent) SpinnerInfo() (SpinnerInfo, bool) {
	return SpinnerInfo{
		Status:         fmt.Sprintf(" Retrying in %v...", e.Duration.Round(time.Second)),
		ResetRendering: true,
	}, true
}

// SpinnerInfo describes the spinner presentation for an event.
// It is a DTO consumed by the UI bridge's spinner coordinator.
type SpinnerInfo struct {
	Status         string // The spinner status text (e.g. " Thinking...")
	WithMetrics    bool   // If true, use StartSpinnerWithMetrics instead of StartSpinnerWithStatus
	ResetRendering bool   // If true, the spinner transition resets the rendering state
}

// ConsentStartedEvent signals that the user is being prompted for tool consent.
type ConsentStartedEvent struct{}

func (e ConsentStartedEvent) Type() string { return "ConsentStartedEvent" }

// ConsentFinishedEvent signals that the user consent prompt has finished.
type ConsentFinishedEvent struct{}

func (e ConsentFinishedEvent) Type() string { return "ConsentFinishedEvent" }
