// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import "path/filepath"

// Paths holds the filesystem paths for a session.
type Paths struct {
	ModeDir            string
	HistoryPath        string
	HistoryArchivePath string
	LogPath            string
	TracePath          string
	CommandsLogPath    string
	TurnsLogPath       string
}

// ResolvePaths determines the session paths based on the home directory and mode.
func ResolvePaths(homeDir string, mode string) *Paths {
	safeMode := filepath.Base(filepath.Clean(mode))
	// Prevent directory traversal or empty mode
	if safeMode == "." || safeMode == ".." || safeMode == "/" || safeMode == "\\" || mode == "" {
		safeMode = "default"
	}

	modeDir := filepath.Join(homeDir, "output", safeMode)
	return &Paths{
		ModeDir:            modeDir,
		HistoryPath:        filepath.Join(modeDir, "history.jsonl"),
		HistoryArchivePath: filepath.Join(modeDir, "history.archive.jsonl"),
		LogPath:            filepath.Join(modeDir, "tokens.log"),
		TracePath:          filepath.Join(modeDir, "tokens.trace.jsonl"),
		CommandsLogPath:    filepath.Join(modeDir, "commands.log"),
		TurnsLogPath:       filepath.Join(modeDir, "turns.log"),
	}
}
