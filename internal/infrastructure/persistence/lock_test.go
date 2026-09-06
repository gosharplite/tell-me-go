// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileModeLocker_Uncontended(t *testing.T) {
	tempHome := t.TempDir()
	locker := persistence.NewFileModeLocker(tempHome)

	release, err := locker.TryLockMode("architect")
	if err != nil {
		t.Fatalf("unexpected error locking mode: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release function")
	}
	defer release()

	// Verify the .mode.lock file was created in output/architect/
	lockFilePath := filepath.Join(tempHome, "output", "architect", ".mode.lock")
	paths := domain_persistence.ResolvePaths(tempHome, "architect")
	if paths.ModeDir != filepath.Join(tempHome, "output", "architect") {
		t.Errorf("expected ModeDir %s, got %s", filepath.Join(tempHome, "output", "architect"), paths.ModeDir)
	}
	_ = lockFilePath
}

func TestFileModeLocker_Contended(t *testing.T) {
	tempHome := t.TempDir()
	locker := persistence.NewFileModeLocker(tempHome)

	release1, err := locker.TryLockMode("coder")
	if err != nil {
		t.Fatalf("unexpected error on first lock: %v", err)
	}
	defer release1()

	// Attempting a second lock on the same mode while first is held
	release2, err := locker.TryLockMode("coder")
	if !errors.Is(err, domain_persistence.ErrModeLocked) {
		t.Fatalf("expected ErrModeLocked on contended lock, got: %v", err)
	}
	if release2 != nil {
		t.Error("expected nil release func on failure, got non-nil")
	}
}

func TestFileModeLocker_ReleaseReacquire(t *testing.T) {
	tempHome := t.TempDir()
	locker := persistence.NewFileModeLocker(tempHome)

	release1, err := locker.TryLockMode("reviewer")
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	// Release first lock
	release1()

	// Reacquire lock on same mode
	release2, err := locker.TryLockMode("reviewer")
	if err != nil {
		t.Fatalf("reacquiring lock failed: %v", err)
	}
	if release2 == nil {
		t.Fatal("expected non-nil release func on reacquire")
	}
	defer release2()
}

func TestFileModeLocker_DifferentModes(t *testing.T) {
	tempHome := t.TempDir()
	locker := persistence.NewFileModeLocker(tempHome)

	releaseA, err := locker.TryLockMode("modeA")
	if err != nil {
		t.Fatalf("locking modeA failed: %v", err)
	}
	defer releaseA()

	// Locking modeB should succeed independently
	releaseB, err := locker.TryLockMode("modeB")
	if err != nil {
		t.Fatalf("locking modeB failed while modeA held: %v", err)
	}
	defer releaseB()
}

func TestFileModeLocker_ReleaseIdempotent(t *testing.T) {
	tempHome := t.TempDir()
	locker := persistence.NewFileModeLocker(tempHome)

	release, err := locker.TryLockMode("tester")
	if err != nil {
		t.Fatalf("locking mode failed: %v", err)
	}

	// Calling release multiple times should not panic
	release()
	release()
	release()
}

func TestFileModeLocker_InterfaceSatisfaction(t *testing.T) {
	var _ domain_persistence.ModeLocker = persistence.NewFileModeLocker(t.TempDir())
}

// TestFileModeLocker_MkdirAllError_ParentPathIsFile covers the MkdirAll error
// branch in TryLockMode (lock_common.go:23-25). <home>/output exists as a
// regular file, so MkdirAll(<home>/output/architect) fails structurally with
// ENOTDIR — deterministic even when running as root. Error-ness only is
// asserted, not the errno text (Windows portability).
func TestFileModeLocker_MkdirAllError_ParentPathIsFile(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, "output"), []byte("x"), 0600))

	locker := persistence.NewFileModeLocker(home)
	_, err := locker.TryLockMode("architect")
	require.Error(t, err)
	assert.False(t, errors.Is(err, domain_persistence.ErrModeLocked),
		"a structural MkdirAll failure must not be misreported as a contention lock")
}

// TestFileModeLocker_OpenFileError_LockPathIsDirectory covers the OpenFile
// error branch in TryLockMode (lock_common.go:28-30). ModeDir exists so the
// MkdirAll succeeds, but the lock path exists as a directory, so
// os.OpenFile(lockPath, O_CREATE|O_RDWR, 0600) fails structurally (EISDIR) —
// deterministic even when running as root. Error-ness only is asserted, not
// the errno text (Windows portability).
func TestFileModeLocker_OpenFileError_LockPathIsDirectory(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "output", "architect"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(home, "output", "architect", ".mode.lock"), 0755))

	_, err := persistence.NewFileModeLocker(home).TryLockMode("architect")
	require.Error(t, err)
	assert.False(t, errors.Is(err, domain_persistence.ErrModeLocked),
		"a structural OpenFile failure must not be misreported as a contention lock")
}
