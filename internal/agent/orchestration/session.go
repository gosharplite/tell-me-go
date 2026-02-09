// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// Session encapsulates the state of a single conversation session.
type Session struct {
	ID        string
	History   services.HistoryManager
	StartTime time.Time
	// PrunedTurns tracks how many turns were removed from history during initialization.
	PrunedTurns int
}

// NewSession creates a new Session state.
func NewSession(h services.HistoryManager) *Session {
	return &Session{
		History:   h,
		StartTime: time.Now(),
	}
}
