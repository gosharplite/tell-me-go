// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitializePaths(t *testing.T) {
	tmp := t.TempDir()
	mode := "test-mode"

	paths, err := InitializePaths(tmp, mode)
	if err != nil {
		t.Fatalf("InitializePaths failed: %v", err)
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
	tmp := t.TempDir()
	homeDir := tmp
	mode := "test-mode"

	paths, err := InitializePaths(homeDir, mode)
	if err != nil {
		t.Fatal(err)
	}

	// Create dummy files
	files := []string{paths.HistoryPath, paths.LogPath, paths.CommandsLogPath}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("test data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Rotate
	err = RotateSession(nil, *paths, 30)
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
	tmp := t.TempDir()
	mode := "test-mode"
	paths, err := InitializePaths(tmp, mode)
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
	err = CleanupOldBackups(*paths, 30)
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

func TestLoadBackupRetentionDays(t *testing.T) {
	tmp := t.TempDir()
	paths := Paths{
		PersistentConfigPath: filepath.Join(tmp, "config.json"),
	}

	// Test default
	if days := LoadBackupRetentionDays(paths); days != 30 {
		t.Errorf("expected default 30, got %d", days)
	}

	// Test override
	if err := os.WriteFile(paths.PersistentConfigPath, []byte(`{"backup_retention_days": "15"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if days := LoadBackupRetentionDays(paths); days != 15 {
		t.Errorf("expected overridden 15, got %d", days)
	}
}

func TestCleanupOldBackups_NoRetention(t *testing.T) {
	err := CleanupOldBackups(Paths{}, 0)
	if err != nil {
		t.Errorf("CleanupOldBackups with 0 retention should not error: %v", err)
	}
}

func TestCleanupOldBackups_NoDir(t *testing.T) {
	tmp := t.TempDir()
	paths := Paths{ModeDir: filepath.Join(tmp, "nonexistent")}
	err := CleanupOldBackups(paths, 30)
	if err != nil {
		t.Errorf("CleanupOldBackups with nonexistent dir should not error: %v", err)
	}
}
