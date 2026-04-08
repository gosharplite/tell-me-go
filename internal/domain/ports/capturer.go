// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/spf13/pflag"
)

// CaptureOptions configures the behavior of the Capturer.
type CaptureOptions struct {
	SkipTTYWait  bool // If true, the capturer will not block waiting for interactive input if empty.
	Raw          bool // If true, disables markdown rendering/special formatting (if applicable).
	UseTUIPrompt bool // If true, uses the interactive TUI prompt with suggestions.
}

// CaptureOption defines a functional option for configuring CaptureOptions.
type CaptureOption func(*CaptureOptions)

// WithSkipTTYWait sets whether the capturer should skip waiting for interactive TTY input.
func WithSkipTTYWait(skip bool) CaptureOption {
	return func(o *CaptureOptions) {
		o.SkipTTYWait = skip
	}
}

// WithRaw sets whether the capturer should use raw output formatting.
func WithRaw(raw bool) CaptureOption {
	return func(o *CaptureOptions) {
		o.Raw = raw
	}
}

// WithTUIPrompt sets whether to use the interactive TUI prompt.
func WithTUIPrompt(tui bool) CaptureOption {
	return func(o *CaptureOptions) {
		o.UseTUIPrompt = tui
	}
}

// Capturer defines the interface for UI interactions that the orchestrator needs.
type Capturer interface {
	IsTTY(v any) bool
	CapturePrompt(ctx context.Context, fs *pflag.FlagSet, opts ...CaptureOption) (string, error)
	// Close performs any necessary cleanup, such as flushing suggestion buffers.
	Close(ctx context.Context) error
}
