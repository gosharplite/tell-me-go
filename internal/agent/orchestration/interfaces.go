// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"flag"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// CaptureOptions configures the behavior of the Capturer.
type CaptureOptions struct {
	SkipTTYWait bool // If true, the capturer will not block waiting for interactive input if empty.
	Raw         bool // If true, disables markdown rendering/special formatting (if applicable).
}

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	IsTTY(v any) bool
	CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts CaptureOptions) (string, error)
}

// Orchestrator defines the entry point for running a chat session.
type Orchestrator interface {
	Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic Capturer) error
}
