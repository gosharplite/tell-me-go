// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Capturer defines the interface for UI interactions that the sessionManager needs.
type Capturer interface {
	ports.Capturer
}

// SessionManager defines the entry point for running a chat session.
type SessionManager interface {
	Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic ports.Capturer) error
	Rollback(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies) error
	RenderHistory(hManager ports.HistoryManager, sCfg ports.SessionConfig, isTTY bool)
}
