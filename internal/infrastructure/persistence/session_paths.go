// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// paths is an alias for persistence.Paths
type paths = persistence.Paths

// InitializePaths creates the necessary directories and returns the Paths for the session.
func InitializePaths(fs FileSystem, homeDir string, mode string) (*persistence.Paths, error) {
	paths := persistence.ResolvePaths(homeDir, mode)
	if err := EnsureDirectories(fs, paths); err != nil {
		return nil, err
	}
	return paths, nil
}

// EnsureDirectories creates the necessary directories for the session.
func EnsureDirectories(fs FileSystem, paths *persistence.Paths) error {
	if err := fs.MkdirAll(paths.ModeDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory [%s]: %w", paths.ModeDir, err)
	}
	return nil
}

// RotateSession archives existing session files and cleans up old backups.
func RotateSession(fs FileSystem, w io.Writer, paths persistence.Paths, retentionDays int) error {
	timestamp := time.Now().Format("20060102_150405")
	outputDir := filepath.Dir(paths.ModeDir)

	// Archive files
	filesToMove := []string{
		paths.HistoryPath,
		paths.HistoryArchivePath,
		paths.LogPath,
		paths.TracePath,
		paths.CommandsLogPath,
		paths.TurnsLogPath,
	}
	backupDir := filepath.Join(outputDir, "backups", timestamp)

	var errs []error
	backupCreated := false
	for _, f := range filesToMove {
		if _, err := fs.Stat(f); err == nil {
			if !backupCreated {
				if err := fs.MkdirAll(backupDir, 0755); err != nil {
					return fmt.Errorf("error creating backup directory: %w", err)
				}
				if w != nil {
					_, _ = fmt.Fprintf(w, "Archiving existing session files to %s\n", backupDir)
				}
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := fs.Rename(f, dest); err != nil {
				errs = append(errs, fmt.Errorf("error archiving %s: %w", f, err))
			}
		}
	}

	// Always execute cleanup regardless of previous file archiving failures
	if cleanupErr := cleanupOldBackups(fs, paths, retentionDays); cleanupErr != nil {
		errs = append(errs, cleanupErr)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// cleanupOldBackups removes backups older than the specified retention days.
func cleanupOldBackups(fs FileSystem, paths persistence.Paths, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}

	outputDir := filepath.Dir(paths.ModeDir)
	backupBaseDir := filepath.Join(outputDir, "backups")
	entries, err := fs.ReadDir(backupBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if !entry.IsDir() || len(entry.Name()) < 15 {
			continue
		}

		folderTime, err := time.Parse("20060102_150405", entry.Name()[:15])
		if err != nil || !folderTime.Before(cutoff) {
			continue
		}

		path := filepath.Join(backupBaseDir, entry.Name())
		if err := fs.RemoveAll(path); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: Failed to cleanup old backup %s: %v\n", path, err)
		}
	}

	return nil
}
