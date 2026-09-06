// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package persistence

import (
	"errors"
	"fmt"
	"os"
	"sync"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"golang.org/x/sys/windows"
)

func lockFile(f *os.File) (func(), error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&ol,
	)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, domain_persistence.ErrModeLocked
		}
		return nil, fmt.Errorf("LockFileEx mode lock: %w", err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			var unlockOl windows.Overlapped
			_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &unlockOl)
			_ = f.Close()
		})
	}
	return release, nil
}
