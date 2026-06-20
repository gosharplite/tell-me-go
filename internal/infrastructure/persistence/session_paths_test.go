// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

func TestInitializePaths(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	mode := "test-mode"
	ctx := context.Background()

	paths, err := initializePaths(ctx, &OSFileSystem{}, tmp, mode)
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
	ctx := context.Background()

	paths, err := initializePaths(ctx, &OSFileSystem{}, homeDir, mode)
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
	err = RotateSession(ctx, &OSFileSystem{}, nil, *paths, 30, nil)
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
	ctx := context.Background()
	paths, err := initializePaths(ctx, &OSFileSystem{}, tmp, mode)
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
	err = cleanupOldBackups(ctx, &OSFileSystem{}, *paths, 30, nil)
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
	err := cleanupOldBackups(context.Background(), &OSFileSystem{}, persistence.Paths{}, 0, nil)
	if err != nil {
		t.Errorf("CleanupOldBackups with 0 retention should not error: %v", err)
	}
}

func TestCleanupOldBackups_NoDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	paths := persistence.Paths{ModeDir: filepath.Join(tmp, "nonexistent")}
	err := cleanupOldBackups(context.Background(), &OSFileSystem{}, paths, 30, nil)
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
	ctx := context.Background()

	fs := &OSFileSystem{}
	err := EnsureDirectories(ctx, fs, paths)
	if err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	if _, err := os.Stat(paths.ModeDir); os.IsNotExist(err) {
		t.Errorf("ModeDir %s was not created", paths.ModeDir)
	}
}

// =============================================================================
// isExpiredBackup edge cases
// =============================================================================

func TestIsExpiredBackup_NonDirectory(t *testing.T) {
	t.Parallel()

	entry := &mockDirEntry{name: "20250101_120000_backup", isDir: false}
	cutoff := time.Now()

	if isExpiredBackup(entry, cutoff) {
		t.Error("non-directory entry should not be considered an expired backup")
	}
}

func TestIsExpiredBackup_ShortName(t *testing.T) {
	t.Parallel()

	entry := &mockDirEntry{name: "short", isDir: true}
	cutoff := time.Now()

	if isExpiredBackup(entry, cutoff) {
		t.Error("entry with name shorter than 15 chars should not be considered an expired backup")
	}
}

func TestIsExpiredBackup_NonTimestampName(t *testing.T) {
	t.Parallel()

	entry := &mockDirEntry{name: "not-a-timestamp_suffix", isDir: true}
	cutoff := time.Now()

	if isExpiredBackup(entry, cutoff) {
		t.Error("entry with non-timestamp prefix should not be considered an expired backup")
	}
}

// =============================================================================
// EnsureDirectories failure
// =============================================================================

func TestEnsureDirectories_MkdirAllFailure(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	m.MkdirAllFunc = func(ctx context.Context, path string, perm os.FileMode) error {
		return errors.New("disk full")
	}

	paths := persistence.ResolvePaths("/home/test", "default")
	err := EnsureDirectories(context.Background(), m, paths)
	if err == nil {
		t.Error("expected error from EnsureDirectories when MkdirAll fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create session directory") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

// =============================================================================
// RotateSession error paths
// =============================================================================

func TestRotateSession_BackupCreationFailure(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	// Make the files exist so RotateSession tries to create the backup dir.
	m.StatFunc = func(ctx context.Context, name string) (os.FileInfo, error) {
		return &mockFileInfo{name: name}, nil
	}
	m.MkdirAllFunc = func(ctx context.Context, path string, perm os.FileMode) error {
		// Fail on backup directory creation specifically.
		if strings.Contains(path, "backups") {
			return errors.New("cannot create backup dir")
		}
		return nil
	}

	paths := persistence.ResolvePaths("/home/test", "default")
	var buf bytes.Buffer
	err := RotateSession(context.Background(), m, &buf, *paths, 7, slog.Default())
	if err == nil {
		t.Error("expected error when backup dir creation fails, got nil")
	}
}

func TestRotateSession_PartialArchiveErrors(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	// Make multiple files exist.
	m.StatFunc = func(ctx context.Context, name string) (os.FileInfo, error) {
		return &mockFileInfo{name: name}, nil
	}
	// First Rename succeeds, second fails.
	renameCount := 0
	m.RenameFunc = func(ctx context.Context, oldpath, newpath string) error {
		renameCount++
		if renameCount == 1 {
			return nil // first file moves fine
		}
		return errors.New("rename failed for second file")
	}

	paths := persistence.ResolvePaths("/home/test", "default")
	var buf bytes.Buffer
	err := RotateSession(context.Background(), m, &buf, *paths, 7, slog.Default())
	if err == nil {
		t.Error("expected error from partial archive failures, got nil")
	}
}

// =============================================================================
// initializePaths failure propagation
// =============================================================================

func TestInitializePaths_EnsureDirectoriesFailure(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	m.MkdirAllFunc = func(ctx context.Context, path string, perm os.FileMode) error {
		return errors.New("cannot create dir")
	}

	_, err := initializePaths(context.Background(), m, "/home/test", "default")
	if err == nil {
		t.Error("expected error when EnsureDirectories fails inside initializePaths, got nil")
	}
}

// =============================================================================
// RotateSession with non-nil writer — asserts archival message is written
// =============================================================================

func TestRotateSession_WithWriter(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	homeDir := tmp
	mode := "test-mode"
	ctx := context.Background()

	paths, err := initializePaths(ctx, &OSFileSystem{}, homeDir, mode)
	if err != nil {
		t.Fatal(err)
	}

	// Create at least one file so the backup directory is created
	if err := os.WriteFile(paths.HistoryPath, []byte("test data"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = RotateSession(ctx, &OSFileSystem{}, &buf, *paths, 30, nil)
	if err != nil {
		t.Fatalf("RotateSession failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Archiving existing session files") {
		t.Errorf("expected archival message in writer output, got: %s", output)
	}
}

// =============================================================================
// cleanupOldBackups with ReadDir non-NotExist error — error propagates
// =============================================================================

func TestCleanupOldBackups_ReadDirError(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	m.ReadDirFunc = func(ctx context.Context, name string) ([]os.DirEntry, error) {
		return nil, os.ErrPermission
	}

	paths := persistence.ResolvePaths("/home/test", "default")
	err := cleanupOldBackups(context.Background(), m, *paths, 7, nil)
	if err == nil {
		t.Error("expected error from ReadDir failure, got nil")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

// =============================================================================
// RotateSession with cleanupOldBackups returning an error
// =============================================================================

func TestRotateSession_CleanupError(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	// Make the files exist so the archive loop proceeds.
	m.StatFunc = func(ctx context.Context, name string) (os.FileInfo, error) {
		return &mockFileInfo{name: name}, nil
	}
	// ReadDir returns a non-NotExist error so cleanupOldBackups fails.
	m.ReadDirFunc = func(ctx context.Context, name string) ([]os.DirEntry, error) {
		return nil, os.ErrPermission
	}

	paths := persistence.ResolvePaths("/home/test", "default")
	var buf bytes.Buffer
	err := RotateSession(context.Background(), m, &buf, *paths, 7, slog.Default())
	if err == nil {
		t.Error("expected error when cleanupOldBackups fails inside RotateSession, got nil")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

// =============================================================================
// RotateSession with no session files — archiveSessionFiles early return
// =============================================================================

func TestRotateSession_NoSessionFiles(t *testing.T) {
	t.Parallel()

	m := newMockFS()
	// All Stat calls return ErrNotExist → hasFiles stays false →
	// archiveSessionFiles returns nil without creating backup dir.
	m.StatFunc = func(ctx context.Context, name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	// Track whether MkdirAll was called — it should NOT be.
	mkdirCalled := false
	m.MkdirAllFunc = func(ctx context.Context, path string, perm os.FileMode) error {
		mkdirCalled = true
		return nil
	}

	paths := persistence.ResolvePaths("/home/test", "default")
	var buf bytes.Buffer
	err := RotateSession(context.Background(), m, &buf, *paths, 7, slog.Default())
	if err != nil {
		t.Fatalf("RotateSession should not error when no files exist: %v", err)
	}
	if mkdirCalled {
		t.Error("MkdirAll should NOT be called when no session files exist")
	}
}
