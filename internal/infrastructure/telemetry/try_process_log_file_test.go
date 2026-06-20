// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryProcessLogFile(t *testing.T) {
	t.Parallel()

	t.Run("valid file", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		sessionDir := filepath.Join(tempDir, "session")
		require.NoError(t, os.MkdirAll(sessionDir, 0755))
		logFile := filepath.Join(sessionDir, "tokens.log")
		require.NoError(t, os.WriteFile(logFile,
			[]byte(`{"cost": 1.5, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)
		seen := make(map[string]bool)
		pricing := GetPricing(context.Background(), sm, tempDir)

		record := ls.tryProcessLogFile(context.Background(), logFile, tempDir, seen, pricing)

		require.NotNil(t, record, "expected a record for a valid log file")
		assert.Contains(t, record.Session, "session/tokens.log")
		assert.Equal(t, 1.5, record.TotalCost)
	})

	t.Run("already seen", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		sessionDir := filepath.Join(tempDir, "session")
		require.NoError(t, os.MkdirAll(sessionDir, 0755))
		logFile := filepath.Join(sessionDir, "tokens.log")
		require.NoError(t, os.WriteFile(logFile,
			[]byte(`{"cost": 1.0, "timestamp": "2023-10-27T10:00:00Z"}`), 0644))

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		// Pre-populate seen with the expected session ID
		seen := map[string]bool{"session/tokens.log": true}
		pricing := GetPricing(context.Background(), sm, tempDir)

		record := ls.tryProcessLogFile(context.Background(), logFile, tempDir, seen, pricing)

		assert.Nil(t, record, "expected nil when session already seen")
	})

	t.Run("stat error", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		// Path to a file that does not exist
		nonexistentLog := filepath.Join(tempDir, "gone", "tokens.log")

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)
		seen := make(map[string]bool)
		pricing := GetPricing(context.Background(), sm, tempDir)

		record := ls.tryProcessLogFile(context.Background(), nonexistentLog, tempDir, seen, pricing)

		assert.Nil(t, record, "expected nil when os.Stat fails")
	})

	t.Run("getSessionID error", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()

		sessionDir := filepath.Join(tempDir, "session")
		require.NoError(t, os.MkdirAll(sessionDir, 0755))
		logFile := filepath.Join(sessionDir, "tokens.log")
		require.NoError(t, os.WriteFile(logFile,
			[]byte(`{"cost": 1.0}`), 0644))

		sm := security.NewSecurityManager(nil)
		sm.RegisterSafePath(tempDir)
		ls := newLedgerStore(sm, "test-model", nil)

		// Override getSessionID to always return an error
		ls.getSessionIDFunc = func(path, globalDir string) (string, error) {
			return "", os.ErrPermission
		}

		seen := make(map[string]bool)
		pricing := GetPricing(context.Background(), sm, tempDir)

		record := ls.tryProcessLogFile(context.Background(), logFile, tempDir, seen, pricing)

		assert.Nil(t, record, "expected nil when getSessionID returns an error")
	})
}
