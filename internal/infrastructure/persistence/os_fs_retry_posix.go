// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package persistence

// fsRetry implements POSIX-specific retry logic for filesystem errors.
// On non-Windows platforms, we return errors immediately.
func fsRetry(op func() error) error {
	return op()
}
