// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Capturer defines the interface for UI interactions that the sessionManager needs.
type Capturer interface {
	ports.Capturer
}

// SessionConfig defines the configuration interface for a session.
// It lives in the consuming package per ADR-003 Rule #1
// (consumer-defined interfaces) — the ADR-056 exit-criterion realignment.
type SessionConfig interface {
	// GetPrompt returns the user's input prompt for the current turn.
	GetPrompt() string

	// GetLastN returns the number of recent history entries to display.
	GetLastN() int

	// IsLastNSet returns true if -l was explicitly passed by the user.
	// When false, GetLastN() returns 0 (default).
	IsLastNSet() bool

	// GetBackN returns the number of turns to roll back when processing
	// a history navigation command.
	GetBackN() int

	// GetRawOutput returns true if markdown rendering should be disabled
	// and output should be displayed as plain text.
	GetRawOutput() bool

	// GetConfigPath returns the filesystem path to the main configuration file.
	GetConfigPath() string

	// GetConfig returns the full application configuration.
	GetConfig() *config.Config
}

// SessionManager defines the entry point for running a chat session.
type SessionManager interface {
	Run(ctx context.Context, sc SessionConfig, sd ports.ChatterComposer, ic ports.Capturer, tuiOutput bool) error
	Rollback(ctx context.Context, sc SessionConfig, sd ports.ChatterComposer) error
	RenderHistory(hManager ports.HistoryManager, sCfg SessionConfig, isTTY bool)
}
