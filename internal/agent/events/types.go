// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Limits defines the operational constraints for the agent.
type Limits struct {
	MaxHistoryTokens int
	MaxToolTurns     int
	MaxHistoryTurns  int
	TieredThreshold  int
}

// ExecutionConfig defines the parallel execution and timeout settings.
type ExecutionConfig struct {
	MaxConcurrent int
	Timeout       time.Duration
}

// ConfigUpdated signals that the agent's configuration has changed.
type ConfigUpdated struct {
	Limits    Limits
	LogFile   string
	Execution ExecutionConfig
}

// TurnStatus contains the data needed for rendering turn status.
type TurnStatus struct {
	Timestamp        time.Time
	CurrentTurns     int
	MaxHistoryTurns  int
	Tokens           int
	MaxHistoryTokens int
	TieredThreshold  int
	Metrics          *llm.Metrics
	IsPostCall       bool
	StartTime        time.Time
}

// TurnStatusEvent carries payload and token metrics for UI display.
type TurnStatusEvent struct {
	Status TurnStatus
}
