// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package persistence

import (
	"strings"
	"time"
)

// fsRetry implements Windows-specific retry logic for transient filesystem errors.
func fsRetry(op func() error) error {
	var lastErr error
	for i := 0; i < 5; i++ {
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		if isWindowsTransientError(err) {
			time.Sleep(50 * time.Millisecond)
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
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") ||
		strings.Contains(msg, "the process cannot access the file because it is being used by another process")
}
