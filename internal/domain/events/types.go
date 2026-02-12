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
	StartTime        time.Time
	SessionCost      float64
	DailyCost        float64
	TaskCost         float64
	TotalM           int64
	TotalH           int64
	TotalO           int64
}

// TurnStatusEvent carries payload and token metrics for UI display.
type TurnStatusEvent struct {
	Status TurnStatus
}
