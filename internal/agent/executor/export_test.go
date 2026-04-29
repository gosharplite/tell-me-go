// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

// Exported wrappers for private symbols used in tests.

// HandleTimeout exposes the private (*safetyDecorator).handleTimeout method.
var HandleTimeout = (*safetyDecorator).handleTimeout

// NewLivenessTimer exposes the private newLivenessTimer constructor.
var NewLivenessTimer = newLivenessTimer
