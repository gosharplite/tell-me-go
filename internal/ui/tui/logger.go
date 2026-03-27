// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"io"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// InitLogger initializes the Bubble Tea background logger.
// It returns a closer function to ensure the log file is flushed and closed.
func InitLogger() (io.Closer, error) {
	logPath := filepath.Join(os.TempDir(), "tell-me-go-tui.log")
	f, err := tea.LogToFile(logPath, "debug")
	if err != nil {
		return nil, err
	}
	return f, nil
}
