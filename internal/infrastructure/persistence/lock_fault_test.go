// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package persistence

import (
	"errors"
	"os"
	"syscall"
	"testing"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockFile_NonContentionFlockError covers the non-EWOULDBLOCK/EAGAIN
// flock error branch in lockFile (lock_posix.go:25). The underlying fd is
// invalidated underneath the *os.File (via syscall.Close) so syscall.Flock
// fails with EBADF — a non-contention errno that must be wrapped, not
// reported as ErrModeLocked. lockFile receives the *os.File as a parameter,
// which is the injection seam. Nothing opens a file between the syscall.Close
// and the lockFile call, so there is no fd-reuse hazard.
func TestLockFile_NonContentionFlockError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "lock-*.flock")
	require.NoError(t, err)
	require.NoError(t, syscall.Close(int(f.Fd())))

	release, err := lockFile(f)
	require.Error(t, err)
	require.False(t, errors.Is(err, domain_persistence.ErrModeLocked),
		"a non-contention flock failure must not be misreported as ErrModeLocked")
	assert.Nil(t, release)
	assert.Contains(t, err.Error(), "flock mode lock")

	// Safe: lockFile already closed the *os.File on the error path; a second
	// Close returns an error at worst, never a panic.
	defer func() { _ = f.Close() }()
}
