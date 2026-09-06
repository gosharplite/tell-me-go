// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package persistence

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"

	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

func lockFile(f *os.File) (func(), error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, domain_persistence.ErrModeLocked
		}
		return nil, fmt.Errorf("flock mode lock: %w", err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}
	return release, nil
}
