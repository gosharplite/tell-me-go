// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"testing"
	"time"
)

// --- CaptureOption functional options ---

func TestCaptureOption_WithSkipTTYWait(t *testing.T) {
	t.Parallel()
	opts := &CaptureOptions{}
	WithSkipTTYWait(true)(opts)
	if !opts.SkipTTYWait {
		t.Error("expected SkipTTYWait to be true")
	}
}

func TestCaptureOption_WithRaw(t *testing.T) {
	t.Parallel()
	opts := &CaptureOptions{}
	WithRaw(true)(opts)
	if !opts.Raw {
		t.Error("expected Raw to be true")
	}
}

func TestCaptureOption_WithTUIPrompt(t *testing.T) {
	t.Parallel()
	opts := &CaptureOptions{}
	WithTUIPrompt(true)(opts)
	if !opts.UseTUIPrompt {
		t.Error("expected UseTUIPrompt to be true")
	}
}

func TestCaptureOption_Composition(t *testing.T) {
	t.Parallel()
	opts := &CaptureOptions{}
	WithSkipTTYWait(true)(opts)
	WithRaw(true)(opts)
	WithTUIPrompt(true)(opts)
	if !opts.SkipTTYWait || !opts.Raw || !opts.UseTUIPrompt {
		t.Error("all options should be true after composition")
	}
}

// --- Sentinel errors ---

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	if ErrHistoryNotFound.Error() == "" {
		t.Error("ErrHistoryNotFound.Error() should be non-empty")
	}
	if ErrTaskNotFound.Error() == "" {
		t.Error("ErrTaskNotFound.Error() should be non-empty")
	}
}

// --- Constants ---

func TestDefaultShutdownTimeout(t *testing.T) {
	t.Parallel()
	if DefaultShutdownTimeout != 1*time.Second {
		t.Errorf("expected DefaultShutdownTimeout to be 1s, got %v", DefaultShutdownTimeout)
	}
}

func TestHealthConstants(t *testing.T) {
	t.Parallel()
	if CompPersistence != Component("persistence") {
		t.Errorf("expected CompPersistence to be 'persistence', got %q", CompPersistence)
	}
	if CompLLMProvider != Component("llm") {
		t.Errorf("expected CompLLMProvider to be 'llm', got %q", CompLLMProvider)
	}
	if CompToolchain != Component("toolchain") {
		t.Errorf("expected CompToolchain to be 'toolchain', got %q", CompToolchain)
	}
}

func TestHealthStatusConstants(t *testing.T) {
	t.Parallel()
	if StatusHealthy != HealthStatus("healthy") {
		t.Errorf("expected StatusHealthy to be 'healthy', got %q", StatusHealthy)
	}
	if StatusDegraded != HealthStatus("degraded") {
		t.Errorf("expected StatusDegraded to be 'degraded', got %q", StatusDegraded)
	}
	if StatusUnhealthy != HealthStatus("unhealthy") {
		t.Errorf("expected StatusUnhealthy to be 'unhealthy', got %q", StatusUnhealthy)
	}
}
