// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build unix

package memory

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

const (
	// flockPollInterval is the sleep between non-blocking lock attempts.
	flockPollInterval = 100 * time.Millisecond
	// flockMaxAttempts bounds the poll budget (~500ms via the injected clock).
	flockMaxAttempts = 5
)

// acquireWriteLock takes a non-blocking LOCK_EX|LOCK_NB on
// ~/.plur/.tmg-write.lock, polling up to the bounded budget via clk.Sleep.
// Returns (release, true) on success; (nil, false) on any failure
// (uncreatable, EWOULDBLOCK budget exhausted, other error) — callers log
// and proceed unlocked. Unix-only; no-op (nil, false) on !unix.
func acquireWriteLock(clk clock.Clock) (release func(), acquired bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	f, err := os.OpenFile(filepath.Join(home, ".plur", ".tmg-write.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false
	}
	for i := 0; i < flockMaxAttempts; i++ {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, true
		}
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			if clk != nil {
				clk.Sleep(flockPollInterval)
			}
			continue
		}
		_ = f.Close()
		return nil, false
	}
	_ = f.Close()
	return nil, false
}
