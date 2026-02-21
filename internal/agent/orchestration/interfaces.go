// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"flag"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	IsTTY(v any) bool
	CapturePrompt(ctx context.Context, fs *flag.FlagSet, lastN int, raw bool) (string, error)
}

// Orchestrator defines the entry point for running a chat session.
type Orchestrator interface {
	Run(ctx context.Context, sc services.SessionConfig, sd services.SessionDependencies, ic Capturer) error
}
