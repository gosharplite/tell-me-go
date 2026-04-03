// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrUserDeclined   = errors.New("user explicitly declined the action")
	ErrSecurityPolicy = errors.New("action blocked by security policy")
)
