// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import "errors"

// Sentinel errors for tool execution. These errors signal terminal
// (non-retryable) failures to the orchestrator.
var (
	// ErrNotImplemented is returned when a tool's functionality has not
	// been implemented yet. The orchestrator treats this as a terminal
	// failure and does not retry.
	ErrNotImplemented = errors.New("not implemented")

	// ErrUserDeclined is returned when the user explicitly declines a
	// tool's consent prompt. The orchestrator treats this as a terminal
	// failure and does not retry.
	ErrUserDeclined = errors.New("user explicitly declined the action")

	// ErrSecurityPolicy is returned when a tool invocation is blocked
	// by the security manager. The orchestrator treats this as a terminal
	// failure and does not retry.
	ErrSecurityPolicy = errors.New("action blocked by security policy")
)
