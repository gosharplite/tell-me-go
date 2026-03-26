// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type historyLoadedMsg struct {
	dtos       []ports.HistoryViewDTO
	nextCursor string
	err        error
}

// RootBrowserModel implements the tea.Model interface for the history browser.
type RootBrowserModel struct {
	provider   ports.UnifiedHistoryProvider
	history    []ports.HistoryViewDTO
	isLoading  bool
	cursor     string
	err        error
}

// NewRootBrowserModel creates a new history browser root model.
func NewRootBrowserModel(provider ports.UnifiedHistoryProvider) *RootBrowserModel {
	return &RootBrowserModel{
		provider:  provider,
		isLoading: true,
	}
}

// Init initializes the model with an asynchronous disk read.
func (m RootBrowserModel) Init() tea.Cmd {
	return fetchHistoryCmd(m.provider, "")
}

// Update handles incoming messages and updates the model state.
func (m RootBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

	case historyLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.history = append(m.history, msg.dtos...)
		m.cursor = msg.nextCursor
		return m, nil
	}

	return m, nil
}

// View renders the current state of the model.
func (m RootBrowserModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\nPress 'q' to quit.", m.err)
	}
	if m.isLoading && len(m.history) == 0 {
		return "Loading history..."
	}

	var sb strings.Builder
	sb.WriteString("=== History Browser (q to quit) ===\n\n")
	for _, dto := range m.history {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", dto.Role, dto.ContentPreview))
	}

	if m.isLoading {
		sb.WriteString("\nLoading more...")
	}

	return sb.String()
}

func fetchHistoryCmd(provider ports.UnifiedHistoryProvider, cursor string) tea.Cmd {
	return func() tea.Msg {
		// Use a 30s timeout for safety, though provider should handle it.
		// In a real TUI, we might want to propagate context from the main loop.
		dtos, nextCursor, err := provider.GetHistoryStream(context.Background(), 20, cursor)
		return historyLoadedMsg{
			dtos:       dtos,
			nextCursor: nextCursor,
			err:        err,
		}
	}
}
