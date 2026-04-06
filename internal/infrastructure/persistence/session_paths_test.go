// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

func TestInitializePaths(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	mode := "test-mode"

	paths, err := initializePaths(&OSFileSystem{}, tmp, mode)
	if err != nil {
		t.Fatalf("initializePaths failed: %v", err)
	}

	expectedDir := filepath.Join(tmp, "output", mode)
	if paths.ModeDir != expectedDir {
		t.Errorf("expected ModeDir %s, got %s", expectedDir, paths.ModeDir)
	}

	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("ModeDir was not created")
	}
}

func TestRotateSession(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	homeDir := tmp
	mode := "test-mode"

	paths, err := initializePaths(&OSFileSystem{}, homeDir, mode)
	if err != nil {
		t.Fatal(err)
	}

	// Create dummy files
	files := []string{
		paths.HistoryPath,
		paths.HistoryArchivePath,
		paths.LogPath,
		paths.TracePath,
		paths.CommandsLogPath,
		paths.TurnsLogPath,
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("test data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Rotate
	err = RotateSession(&OSFileSystem{}, nil, *paths, 30)
	if err != nil {
		t.Fatalf("RotateSession failed: %v", err)
	}

	// Verify original files are gone
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("file %s should have been moved", f)
		}
	}

	// Verify backup exists
	backupBase := filepath.Join(tmp, "output", "backups")
	entries, err := os.ReadDir(backupBase)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 backup directory, got %d", len(entries))
	}
}

func TestCleanupOldBackups(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	mode := "test-mode"
	paths, err := initializePaths(&OSFileSystem{}, tmp, mode)
	if err != nil {
		t.Fatal(err)
	}

	backupBase := filepath.Join(tmp, "output", "backups")

	// Create an old backup (31 days ago)
	oldTimestamp := time.Now().AddDate(0, 0, -31).Format("20060102_150405")
	oldDir := filepath.Join(backupBase, oldTimestamp)
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a fresh backup
	newTimestamp := time.Now().Format("20060102_150405")
	newDir := filepath.Join(backupBase, newTimestamp)
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Cleanup with 30 day retention
	err = cleanupOldBackups(&OSFileSystem{}, *paths, 30)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old backup %s should have been deleted", oldDir)
	}
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		t.Errorf("new backup %s should still exist", newDir)
	}
}

func TestCleanupOldBackups_NoRetention(t *testing.T) {
	t.Parallel()
	err := cleanupOldBackups(&OSFileSystem{}, persistence.Paths{}, 0)
	if err != nil {
		t.Errorf("CleanupOldBackups with 0 retention should not error: %v", err)
	}
}

func TestCleanupOldBackups_NoDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := persistence.Paths{ModeDir: filepath.Join(tmp, "nonexistent")}
	err := cleanupOldBackups(&OSFileSystem{}, paths, 30)
	if err != nil {
		t.Errorf("CleanupOldBackups with nonexistent dir should not error: %v", err)
	}
}

func TestResolvePaths(t *testing.T) {
	t.Parallel()
	homeDir := "/home/user"

	tests := []struct {
		name     string
		mode     string
		expected string
	}{
		{"standard mode", "assistant", filepath.Join(homeDir, "output", "assistant")},
		{"path traversal attempt", "../../../etc/passwd", filepath.Join(homeDir, "output", "passwd")},
		{"nested path traversal", "subdir/../../hidden", filepath.Join(homeDir, "output", "hidden")},
		{"empty mode", "", filepath.Join(homeDir, "output", "default")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := persistence.ResolvePaths(homeDir, tt.mode)
			if paths.ModeDir != tt.expected {
				t.Errorf("expected ModeDir %s, got %s", tt.expected, paths.ModeDir)
			}
			// Verify turns.log is also correctly nested
			expectedLog := filepath.Join(tt.expected, "turns.log")
			if paths.TurnsLogPath != expectedLog {
				t.Errorf("expected TurnsLogPath %s, got %s", expectedLog, paths.TurnsLogPath)
			}
		})
	}
}

func TestEnsureDirectories(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	mode := "test-ensure"
	paths := persistence.ResolvePaths(tmp, mode)

	fs := &OSFileSystem{}
	err := EnsureDirectories(fs, paths)
	if err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	if _, err := os.Stat(paths.ModeDir); os.IsNotExist(err) {
		t.Errorf("ModeDir %s was not created", paths.ModeDir)
	}
}
