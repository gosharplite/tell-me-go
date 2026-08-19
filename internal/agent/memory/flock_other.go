// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !unix

package memory

import (
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// acquireWriteLock is a no-op on non-Unix platforms: the advisory flock is
// Unix-only, so callers always proceed unlocked (fail-open).
func acquireWriteLock(clk clock.Clock) (release func(), acquired bool) {
	return nil, false
}
