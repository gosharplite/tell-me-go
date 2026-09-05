// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence_test

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

func TestErrModeLocked(t *testing.T) {
	if persistence.ErrModeLocked == nil {
		t.Fatal("expected ErrModeLocked to be non-nil")
	}
	want := "mode is locked by another process"
	if got := persistence.ErrModeLocked.Error(); got != want {
		t.Errorf("ErrModeLocked.Error() = %q, want %q", got, want)
	}
}

type mockModeLocker struct {
	lockedModes map[string]bool
}

func (m *mockModeLocker) TryLockMode(mode string) (func(), error) {
	if m.lockedModes[mode] {
		return nil, persistence.ErrModeLocked
	}
	if m.lockedModes == nil {
		m.lockedModes = make(map[string]bool)
	}
	m.lockedModes[mode] = true
	release := func() {
		delete(m.lockedModes, mode)
	}
	return release, nil
}

func TestModeLocker_InterfaceSatisfaction(t *testing.T) {
	var locker persistence.ModeLocker = &mockModeLocker{}

	release, err := locker.TryLockMode("coder")
	if err != nil {
		t.Fatalf("unexpected error locking mode: %v", err)
	}
	if release == nil {
		t.Fatal("expected non-nil release func")
	}

	// Try locking the same mode again, should fail with ErrModeLocked
	_, err = locker.TryLockMode("coder")
	if !errors.Is(err, persistence.ErrModeLocked) {
		t.Errorf("expected ErrModeLocked, got %v", err)
	}

	// Release lock
	release()

	// Now locking should succeed again
	release2, err := locker.TryLockMode("coder")
	if err != nil {
		t.Fatalf("unexpected error after release: %v", err)
	}
	if release2 == nil {
		t.Fatal("expected non-nil release func")
	}
	release2()
}
