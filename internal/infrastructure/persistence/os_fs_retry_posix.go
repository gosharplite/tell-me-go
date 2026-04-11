// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package persistence

import "context"

// fsRetry implements POSIX-specific retry logic for filesystem errors.
// On non-Windows platforms, we return errors immediately.
func fsRetry(ctx context.Context, op func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return op()
}
