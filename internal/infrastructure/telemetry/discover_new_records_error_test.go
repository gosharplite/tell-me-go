// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscoverNewRecords_NonNotExistError exercises the !os.IsNotExist(err)
// branch in discoverNewRecords (ledger.go:169-172). When processLogFile
// returns an error that is NOT os.IsNotExist (e.g., EACCES from os.Open on
// an unreadable file), the function logs a warning and continues to the
// next file instead of panicking or returning early.
func TestDiscoverNewRecords_NonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod 0000 not effective on Windows")
	}

	t.Run("one_unreadable_one_readable", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a readable directory with a valid tokens.log.
		readableDir := filepath.Join(tempDir, "readable")
		require.NoError(t, os.MkdirAll(readableDir, 0755))
		readableLog := filepath.Join(readableDir, "tokens.log")
		require.NoError(t, os.WriteFile(readableLog,
			[]byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

		// Create an unreadable tokens.log (mode 0000).
		// os.Stat succeeds on Linux (only needs search permission on parent dirs),
		// but os.Open (called by parseUsage) fails with EACCES, which is NOT
		// os.IsNotExist, exercising the target branch.
		unreadableDir := filepath.Join(tempDir, "unreadable")
		require.NoError(t, os.MkdirAll(unreadableDir, 0755))
		unreadableLog := filepath.Join(unreadableDir, "tokens.log")
		require.NoError(t, os.WriteFile(unreadableLog,
			[]byte(`{"cost": 2.0}`), 0000))

		// Place unreadable first to ensure it hits the error branch before
		// the readable file is processed.
		files := []string{unreadableLog, readableLog}

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		seen := make(map[string]bool)
		pricing := GetPricing(context.Background(), sm, tempDir)

		discovered := ls.discoverNewRecords(context.Background(), files, tempDir, seen, pricing)

		// Only the readable file should produce a record.
		require.Len(t, discovered, 1, "only the readable file should be discovered")
		assert.Contains(t, discovered[0].Session, "readable/tokens.log")
	})

	t.Run("all_unreadable", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create a single tokens.log with mode 0000.
		unreadableDir := filepath.Join(tempDir, "unreadable")
		require.NoError(t, os.MkdirAll(unreadableDir, 0755))
		unreadableLog := filepath.Join(unreadableDir, "tokens.log")
		require.NoError(t, os.WriteFile(unreadableLog,
			[]byte(`{"cost": 2.0}`), 0000))

		files := []string{unreadableLog}

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		seen := make(map[string]bool)
		pricing := GetPricing(context.Background(), sm, tempDir)

		// Must not panic.
		discovered := ls.discoverNewRecords(context.Background(), files, tempDir, seen, pricing)

		// No records should be produced — the unreadable file's error is
		// caught by the !os.IsNotExist branch, which logs and continues.
		require.Empty(t, discovered, "no records should be discovered from unreadable file")
	})
}
