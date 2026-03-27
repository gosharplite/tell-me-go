// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"io"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// loggerCloser handles the restoration of global logger state and file closing.
type loggerCloser struct {
	file     *os.File
	oldOut   io.Writer
	oldFlags int
}

// Close restores the standard logger state and closes the log file.
func (c *loggerCloser) Close() error {
	log.SetOutput(c.oldOut)
	log.SetFlags(c.oldFlags)
	return c.file.Close()
}

// InitLogger initializes the Bubble Tea background logger.
// It returns an io.Closer that captures and restores the global logger state
// to prevent silent logging failures after the TUI session ends.
func InitLogger() (io.Closer, error) {
	oldOut := log.Writer()
	oldFlags := log.Flags()

	logPath := filepath.Join(os.TempDir(), "tell-me-go-tui.log")
	f, err := tea.LogToFile(logPath, "debug")
	if err != nil {
		return nil, err
	}

	return &loggerCloser{
		file:     f,
		oldOut:   oldOut,
		oldFlags: oldFlags,
	}, nil
}
