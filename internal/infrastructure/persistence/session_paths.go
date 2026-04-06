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
	modeDir := filepath.Join(homeDir, "output", mode)
	if err := fs.MkdirAll(modeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory [%s]: %w", modeDir, err)
	}

	return &persistence.Paths{
		ModeDir:            modeDir,
		HistoryPath:        filepath.Join(modeDir, "history.jsonl"),
		HistoryArchivePath: filepath.Join(modeDir, "history.archive.jsonl"),
		LogPath:            filepath.Join(modeDir, "tokens.log"),
		TracePath:          filepath.Join(modeDir, "tokens.trace.jsonl"),
		CommandsLogPath:    filepath.Join(modeDir, "commands.log"),
		TurnsLogPath:       filepath.Join(modeDir, "turns.log"),
	}, nil
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
