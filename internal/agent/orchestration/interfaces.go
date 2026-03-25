// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	ports.Capturer
}

// Orchestrator defines the entry point for running a chat session.
type Orchestrator interface {
	Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic ports.Capturer) error
	Rollback(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies) error
	RenderHistory(hManager ports.HistoryManager, sCfg ports.SessionConfig, isTTY bool)
}
