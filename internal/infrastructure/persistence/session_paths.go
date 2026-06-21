// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// initializePaths creates the necessary directories and returns the Paths for the session.
func initializePaths(ctx context.Context, fs FileSystem, homeDir string, mode string) (*persistence.Paths, error) {
	paths := persistence.ResolvePaths(homeDir, mode)
	if err := EnsureDirectories(ctx, fs, paths); err != nil {
		return nil, err
	}
	return paths, nil
}

// EnsureDirectories creates the necessary directories for the session.
func EnsureDirectories(ctx context.Context, fs FileSystem, paths *persistence.Paths) error {
	if err := fs.MkdirAll(ctx, paths.ModeDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory [%s]: %w", paths.ModeDir, err)
	}
	return nil
}

// RotateSession archives existing session files and cleans up old backups.
func RotateSession(ctx context.Context, fs FileSystem, w io.Writer, paths persistence.Paths, retentionDays int, logger *slog.Logger) error {
	timestamp := time.Now().Format("20060102_150405")

	var errs []error
	errs = append(errs, archiveSessionFiles(ctx, fs, w, paths, timestamp)...)

	if cleanupErr := cleanupOldBackups(ctx, fs, paths, retentionDays, logger); cleanupErr != nil {
		errs = append(errs, cleanupErr)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// hasFilesToArchive checks whether any of the given files exist on the filesystem.
// It short-circuits on the first existing file found.
func hasFilesToArchive(ctx context.Context, fs FileSystem, filesToMove []string) bool {
	for _, f := range filesToMove {
		if _, err := fs.Stat(ctx, f); err == nil {
			return true
		}
	}
	return false
}

// createBackupDir creates a timestamped backup directory and optionally
// writes an archival message to the provided writer.
func createBackupDir(ctx context.Context, fs FileSystem, w io.Writer, paths persistence.Paths, timestamp string) (string, error) {
	outputDir := filepath.Dir(paths.ModeDir)
	backupDir := filepath.Join(outputDir, "backups", timestamp)

	if err := fs.MkdirAll(ctx, backupDir, 0755); err != nil {
		return "", fmt.Errorf("error creating backup directory: %w", err)
	}
	if w != nil {
		_, _ = fmt.Fprintf(w, "Archiving existing session files to %s\n", backupDir)
	}
	return backupDir, nil
}

// isReportableRenameError reports whether a Rename error should be
// surfaced. os.ErrNotExist is silently ignored (TOCTOU race between
// hasFilesToArchive check and the actual rename).
func isReportableRenameError(err error) bool {
	return err != nil && !os.IsNotExist(err)
}

// moveFilesToBackup moves each existing file in filesToMove into the backupDir.
// Missing files (os.ErrNotExist) are silently skipped. Rename errors are
// accumulated and returned. Returns nil when all files were moved successfully
// or no files existed.
func moveFilesToBackup(ctx context.Context, fs FileSystem, filesToMove []string, backupDir string) []error {
	var errs []error
	for _, f := range filesToMove {
		dest := filepath.Join(backupDir, filepath.Base(f))
		if err := fs.Rename(ctx, f, dest); isReportableRenameError(err) {
			errs = append(errs, fmt.Errorf("error archiving %s: %w", f, err))
		}
	}
	return errs
}

// archiveSessionFiles moves session files into a timestamped backup directory.
// It returns accumulated errors from individual file moves, or a hard error
// (as a single-element slice) if backup directory creation fails.
func archiveSessionFiles(ctx context.Context, fs FileSystem, w io.Writer, paths persistence.Paths, timestamp string) []error {
	filesToMove := []string{
		paths.HistoryPath,
		paths.HistoryArchivePath,
		paths.LogPath,
		paths.TracePath,
		paths.CommandsLogPath,
		paths.TurnsLogPath,
	}

	if !hasFilesToArchive(ctx, fs, filesToMove) {
		return nil
	}

	backupDir, err := createBackupDir(ctx, fs, w, paths, timestamp)
	if err != nil {
		return []error{err}
	}

	return moveFilesToBackup(ctx, fs, filesToMove, backupDir)
}

// cleanupOldBackups removes backups older than the specified retention days.
func cleanupOldBackups(ctx context.Context, fs FileSystem, paths persistence.Paths, retentionDays int, logger *slog.Logger) error {
	if retentionDays <= 0 {
		return nil
	}

	backupBaseDir := filepath.Join(filepath.Dir(paths.ModeDir), "backups")
	entries, err := fs.ReadDir(ctx, backupBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if !isExpiredBackup(entry, cutoff) {
			continue
		}
		path := filepath.Join(backupBaseDir, entry.Name())
		if rmErr := fs.RemoveAll(ctx, path); rmErr != nil && logger != nil {
			logger.Warn("Failed to cleanup old backup",
				slog.String("path", path),
				slog.Any("error", rmErr),
			)
		}
	}
	return nil
}

// isExpiredBackup reports whether a directory entry is a timestamp-named
// backup folder older than the cutoff. Returns false for non-conforming names.
func isExpiredBackup(entry os.DirEntry, cutoff time.Time) bool {
	if !entry.IsDir() || len(entry.Name()) < 15 {
		return false
	}
	folderTime, err := time.Parse("20060102_150405", entry.Name()[:15])
	if err != nil {
		return false
	}
	return folderTime.Before(cutoff)
}
