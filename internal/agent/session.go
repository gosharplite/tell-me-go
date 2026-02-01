// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/history"
)

// Session encapsulates the state of a single conversation session.
type Session struct {
	History   *history.Manager
	StartTime time.Time
	// PrunedTurns tracks how many turns were removed from history during initialization.
	PrunedTurns int
}

// NewSession creates a new Session state.
func NewSession(h *history.Manager) *Session {
	return &Session{
		History:   h,
		StartTime: time.Now(),
	}
}
