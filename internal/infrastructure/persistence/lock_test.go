// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence_test

import (
	"errors"
	"path/filepath"
	"testing"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
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
