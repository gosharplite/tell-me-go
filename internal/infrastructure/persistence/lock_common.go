// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"os"
	"path/filepath"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

type FileModeLocker struct {
	homeDir string
}

func NewFileModeLocker(homeDir string) *FileModeLocker {
	return &FileModeLocker{homeDir: homeDir}
}

func (l *FileModeLocker) TryLockMode(mode string) (func(), error) {
	paths := domain_persistence.ResolvePaths(l.homeDir, mode)
	if err := os.MkdirAll(paths.ModeDir, 0755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(paths.ModeDir, ".mode.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	return lockFile(f)
}
