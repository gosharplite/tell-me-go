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
	skipTTYWait bool // If true, the capturer will not block waiting for interactive input if empty.
	raw         bool // If true, disables markdown rendering/special formatting (if applicable).
}

// SkipTTYWait returns true if the capturer should skip waiting for interactive input.
func (o *CaptureOptions) SkipTTYWait() bool { return o.skipTTYWait }

// Raw returns true if the capturer should use raw formatting.
func (o *CaptureOptions) Raw() bool { return o.raw }

// CaptureOption defines a functional option for configuring CaptureOptions.
type CaptureOption func(*CaptureOptions)

// WithSkipTTYWait sets whether the capturer should skip waiting for interactive TTY input.
func WithSkipTTYWait(skip bool) CaptureOption {
	return func(o *CaptureOptions) {
		o.skipTTYWait = skip
	}
}

// WithRaw sets whether the capturer should use raw output formatting.
func WithRaw(raw bool) CaptureOption {
	return func(o *CaptureOptions) {
		o.raw = raw
	}
}

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	IsTTY(v any) bool
	CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...CaptureOption) (string, error)
}

// Orchestrator defines the entry point for running a chat session.
type Orchestrator interface {
	Run(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies, ic Capturer) error
	Rollback(ctx context.Context, sc ports.SessionConfig, sd ports.SessionDependencies) error
	RenderHistory(hManager ports.HistoryManager, sCfg ports.SessionConfig, isTTY bool)
}
