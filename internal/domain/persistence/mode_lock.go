// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import "errors"

// ErrModeLocked is returned when a mode directory is already locked by another process.
var ErrModeLocked = errors.New("mode is locked by another process")

// ModeLocker defines the port for acquiring an exclusive, non-blocking lock on a mode directory.
// release is a cleanup function to release the lock (primarily for test teardown).
type ModeLocker interface {
	TryLockMode(mode string) (release func(), err error)
}
