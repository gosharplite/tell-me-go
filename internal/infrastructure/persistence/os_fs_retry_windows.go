// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package persistence

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// fsRetry implements Windows-specific retry logic for transient filesystem errors.
func fsRetry(ctx context.Context, op func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var lastErr error
	delay := 50 * time.Millisecond
	for i := 0; i < 5; i++ {
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		if isWindowsTransientError(err) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
			continue
		}
		return err
	}
	return lastErr
}

// isWindowsTransientError checks if the error is a known transient error on Windows.
func isWindowsTransientError(err error) bool {
	if err == nil {
		return false
	}

	var errno windows.Errno
	if errors.As(err, &errno) {
		return errno == windows.ERROR_ACCESS_DENIED || errno == windows.ERROR_SHARING_VIOLATION
	}

	// Secondary fallback for non-errno wrapped errors
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "the process cannot access the file because it is being used by another process")
}
