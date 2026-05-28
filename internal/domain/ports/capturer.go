// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
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
	// IsTTY reports whether v is connected to an interactive terminal.
	// It accepts an io.Writer (typically os.Stdout or os.Stdin) and returns
	// true if the writer is a character device capable of interactive I/O.
	IsTTY(v any) bool

	// CapturePrompt presents the user with an interactive prompt and returns
	// the input string. It blocks until the user submits input or the context
	// is cancelled.
	//
	// Options (WithSkipTTYWait, WithRaw, WithTUIPrompt) modify capture
	// behavior. When no options are provided, CapturePrompt uses sensible
	// defaults for the current terminal configuration.
	//
	// The returned error is non-nil only when the context is cancelled or
	// the underlying input stream encounters an irrecoverable error.
	CapturePrompt(ctx context.Context, args []string, opts ...CaptureOption) (string, error)

	// Confirm asks the user for a yes/no confirmation.
	Confirm(ctx context.Context, message string) (bool, error)
	// Close performs any necessary cleanup, such as flushing suggestion buffers.
	Close(ctx context.Context) error
}
