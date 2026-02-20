// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import "errors"

var (
	ErrNotImplemented  = errors.New("not implemented")
	ErrToolCircuitOpen = errors.New("tool circuit breaker is open")
	ErrUserDeclined    = errors.New("user explicitly declined the action")
	ErrSecurityPolicy  = errors.New("action blocked by security policy")
)
