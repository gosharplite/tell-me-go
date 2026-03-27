// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// InitLogger initializes the Bubble Tea background logger.
// It returns a closer function to ensure the log file is flushed and closed.
func InitLogger() (io.Closer, error) {
	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		return nil, err
	}
	return f, nil
}
