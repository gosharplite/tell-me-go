// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// TurnStatus contains the data needed for rendering turn status.
type TurnStatus struct {
	Timestamp        time.Time
	CurrentTurns     int
	MaxHistoryTurns  int
	Tokens           int
	MaxHistoryTokens int
	Metrics          *llm.Metrics
	IsPostCall       bool
	StartTime        time.Time
}

// TurnStatusEvent carries payload and token metrics for UI display.
type TurnStatusEvent struct {
	Status TurnStatus
}
