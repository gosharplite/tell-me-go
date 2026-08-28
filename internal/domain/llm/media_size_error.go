// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import "fmt"

// MediaSizeKind identifies the media axis being guarded.
type MediaSizeKind int

const (
	// MediaSizePerImage guards image input dimensions.
	MediaSizePerImage MediaSizeKind = iota
)

// MediaSizeMode identifies the dimension being enforced.
type MediaSizeMode int

const (
	// MediaSizeModeLongestEdge enforces the longest edge (max(width, height)).
	MediaSizeModeLongestEdge MediaSizeMode = iota
)

// MediaSizeError is a typed, terminal media-dimension error. The message is
// pre-rendered at construction (stable Error() output). It unwraps to
// llm.ErrTerminal so IsTerminal/IsFatal classify it as non-retryable.
type MediaSizeError struct {
	Kind   MediaSizeKind
	Mode   MediaSizeMode
	Cap    int
	Actual int
	msg    string // pre-rendered at construction; unexported
}

// NewMediaSizeError constructs a MediaSizeError with a pre-rendered message.
// This is the ONLY construction path — the msg field is unexported, so
// struct literals are not supported.
func NewMediaSizeError(kind MediaSizeKind, mode MediaSizeMode, cap, actual int) *MediaSizeError {
	var msg string
	switch {
	case kind == MediaSizePerImage && mode == MediaSizeModeLongestEdge:
		msg = fmt.Sprintf("image exceeds %d px on the longest edge: got %d px", cap, actual)
	default:
		msg = fmt.Sprintf("media size exceeds limit: cap %d, actual %d", cap, actual)
	}
	return &MediaSizeError{
		Kind:   kind,
		Mode:   mode,
		Cap:    cap,
		Actual: actual,
		msg:    msg,
	}
}

// Error returns the pre-rendered message.
func (e *MediaSizeError) Error() string {
	return e.msg
}

// Unwrap returns llm.ErrTerminal (gateway.go:17), making the error terminal:
// IsTerminal/IsFatal are true, ShouldRetry declines, no retry/failover.
func (e *MediaSizeError) Unwrap() error {
	return ErrTerminal
}
