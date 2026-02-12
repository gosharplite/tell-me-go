// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Paths holds the filesystem paths for a session.
type Paths struct {
	ModeDir              string
	HistoryPath          string
	LogPath              string
	CommandsLogPath      string
	SafePathsPath        string
	ReadPathsPath        string
	BypassPath           string
	PersistentConfigPath string
}

// InitializePaths creates the necessary directories and returns the Paths for the session.
func InitializePaths(homeDir string, mode string) (*Paths, error) {
	modeDir := filepath.Join(homeDir, "output", mode)
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory [%s]: %v", modeDir, err)
	}

	return &Paths{
		ModeDir:              modeDir,
		HistoryPath:          filepath.Join(modeDir, "history.json"),
		LogPath:              filepath.Join(modeDir, "tokens.log"),
		CommandsLogPath:      filepath.Join(modeDir, "commands.log"),
		SafePathsPath:        filepath.Join(modeDir, "safepaths.json"),
		ReadPathsPath:        filepath.Join(modeDir, "readpaths.json"),
		BypassPath:           filepath.Join(modeDir, "bypass.log"),
		PersistentConfigPath: filepath.Join(modeDir, "config.json"),
	}, nil
}

// RotateSession archives existing session files and cleans up old backups.
func RotateSession(w io.Writer, paths Paths, retentionDays int) error {
	timestamp := time.Now().Format("20060102_150405")
	outputDir := filepath.Dir(paths.ModeDir)

	// Archive files
	filesToMove := []string{paths.HistoryPath, paths.LogPath, paths.CommandsLogPath}
	backupDir := filepath.Join(outputDir, "backups", timestamp)

	backupCreated := false
	for _, f := range filesToMove {
		if _, err := os.Stat(f); err == nil {
			if !backupCreated {
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					return fmt.Errorf("error creating backup directory: %w", err)
				}
				if w != nil {
					fmt.Fprintf(w, "Archiving existing session files to %s\n", backupDir)
				}
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := os.Rename(f, dest); err != nil {
				fmt.Fprintf(os.Stderr, "Error archiving %s: %v\n", f, err)
			}
		}
	}

	// Cleanup old backups
	return cleanupOldBackups(paths, retentionDays)
}

// cleanupOldBackups removes backups older than the specified retention days.
func cleanupOldBackups(paths Paths, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}

	outputDir := filepath.Dir(paths.ModeDir)
	backupBaseDir := filepath.Join(outputDir, "backups")
	entries, err := os.ReadDir(backupBaseDir)
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
		if err := os.RemoveAll(path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to cleanup old backup %s: %v\n", path, err)
		}
	}

	return nil
}

// LoadBackupRetentionDays loads the retention days from the persistent config.
func LoadBackupRetentionDays(paths Paths) int {
	retentionDays := 30
	data, err := os.ReadFile(paths.PersistentConfigPath)
	if err != nil {
		return retentionDays
	}

	var cfg map[string]string
	if err := json.Unmarshal(data, &cfg); err == nil {
		if val, ok := cfg["backup_retention_days"]; ok {
			if days, err := strconv.Atoi(val); err == nil {
				return days
			}
		}
	}
	return retentionDays
}
