// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

// Paths holds the filesystem paths for a session.
type Paths struct {
	ModeDir            string
	HistoryPath        string
	HistoryArchivePath string
	LogPath            string
	CommandsLogPath    string
}
