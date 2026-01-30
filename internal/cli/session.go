// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (a *App) archiveSessionFilesWithTimestamp(homeDir, timestamp string, filesToMove ...string) {
	backupDir := filepath.Join(homeDir, "output", "backups", timestamp)

	backupCreated := false
	for _, f := range filesToMove {
		if _, err := os.Stat(f); err == nil {
			if !backupCreated {
				if err := os.MkdirAll(backupDir, 0755); err != nil {
					func() {
						a.sm.TerminalLock()
						defer a.sm.TerminalUnlock()
						fmt.Fprintf(a.Stderr, "Error creating backup directory: %v\n", err)
					}()
					return
				}
				fmt.Fprintf(a.Stdout, "Archiving existing session files to %s\n", backupDir)
				backupCreated = true
			}
			dest := filepath.Join(backupDir, filepath.Base(f))
			if err := os.Rename(f, dest); err != nil {
				func() {
					a.sm.TerminalLock()
					defer a.sm.TerminalUnlock()
					fmt.Fprintf(a.Stderr, "Error archiving %s: %v\n", f, err)
				}()
			}
		}
	}
}

func (a *App) cleanupOldBackups(homeDir, mode string) {
	backupBaseDir := filepath.Join(homeDir, "output", "backups")
	entries, err := os.ReadDir(backupBaseDir)
	if err != nil {
		return // Likely doesn't exist yet
	}

	retentionDays := 30
	configPath := filepath.Join(homeDir, "output", mode, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg map[string]string
		if err := json.Unmarshal(data, &cfg); err == nil {
			if val, ok := cfg["backup_retention_days"]; ok {
				if days, err := strconv.Atoi(val); err == nil {
					retentionDays = days
				}
			}
		}
	}

	if retentionDays <= 0 {
		return // 0 or negative means keep forever
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Format: YYYYMMDD_HHMMSS (15 chars)
		if len(entry.Name()) < 15 {
			continue
		}

		folderTime, err := time.Parse("20060102_150405", entry.Name()[:15])
		if err != nil {
			continue
		}

		if folderTime.Before(cutoff) {
			path := filepath.Join(backupBaseDir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				func() {
					a.sm.TerminalLock()
					defer a.sm.TerminalUnlock()
					fmt.Fprintf(a.Stderr, "Warning: Failed to cleanup old backup %s: %v\n", path, err)
				}()
			}
		}
	}
}
