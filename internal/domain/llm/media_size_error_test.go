// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"errors"
	"testing"
)

func TestNewMediaSizeError_Message(t *testing.T) {
	err := NewMediaSizeError(MediaSizePerImage, MediaSizeModeLongestEdge, 2048, 2049)
	if got, want := err.Error(), "image exceeds 2048 px on the longest edge: got 2049 px"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	// Any other Kind/Mode combination falls back to the generic message,
	// which still contains both cap and actual.
	fallback := NewMediaSizeError(MediaSizeKind(99), MediaSizeMode(99), 2048, 2049)
	if got, want := fallback.Error(), "media size exceeds limit: cap 2048, actual 2049"; got != want {
		t.Errorf("fallback Error() = %q, want %q", got, want)
	}
}

func TestMediaSizeError_Fields(t *testing.T) {
	err := NewMediaSizeError(MediaSizePerImage, MediaSizeModeLongestEdge, 2048, 2049)
	var typed *MediaSizeError
	if !errors.As(err, &typed) {
		t.Fatalf("errors.As(err, &typed) = false, want true")
	}
	if typed.Kind != MediaSizePerImage {
		t.Errorf("Kind = %d, want MediaSizePerImage (%d)", typed.Kind, MediaSizePerImage)
	}
	if typed.Mode != MediaSizeModeLongestEdge {
		t.Errorf("Mode = %d, want MediaSizeModeLongestEdge (%d)", typed.Mode, MediaSizeModeLongestEdge)
	}
	if typed.Cap != 2048 {
		t.Errorf("Cap = %d, want 2048", typed.Cap)
	}
	if typed.Actual != 2049 {
		t.Errorf("Actual = %d, want 2049", typed.Actual)
	}
}

func TestMediaSizeError_UnwrapTerminal(t *testing.T) {
	err := NewMediaSizeError(MediaSizePerImage, MediaSizeModeLongestEdge, 2048, 2049)
	if !errors.Is(err, ErrTerminal) {
		t.Errorf("errors.Is(err, ErrTerminal) = false, want true")
	}
	if !IsTerminal(err) {
		t.Errorf("IsTerminal(err) = false, want true")
	}
	if IsTransient(err) {
		t.Errorf("IsTransient(err) = true, want false")
	}
}

func TestMediaSizeError_Classification(t *testing.T) {
	// The terminal non-retryable contract is the Unwrap → ErrTerminal chain
	// consumed by IsTerminal/ShouldRetry's IsFatal gate; ClassifyLLMError's
	// catch-all category for this error is pre-existing behavior and is
	// intentionally not pinned (adding an LLMError category is forbidden).
	err := NewMediaSizeError(MediaSizePerImage, MediaSizeModeLongestEdge, 2048, 2049)
	if got := ClassifyLLMError(err); got == LLMErrorRateLimited {
		t.Errorf("ClassifyLLMError(err) = LLMErrorRateLimited, want any non-rate-limited category")
	}
}
