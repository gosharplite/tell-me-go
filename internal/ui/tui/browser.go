// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)   // Blue
	modelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)   // Green
	thoughtStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true) // Gray
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))              // Yellow
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Padding(0, 1)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	archivedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
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
	viewport   viewport.Model
	isLoading  bool
	ready      bool
	cursor     string
	err        error
	width      int
	height     int
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
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		footerHeight := 1
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-footerHeight)
			m.viewport.HighPerformanceRendering = false
			m.viewport.SetContent(m.renderHistory())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - footerHeight
		}

	case historyLoadedMsg:
		m.isLoading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.history = append(m.history, msg.dtos...)
		m.cursor = msg.nextCursor
		m.viewport.SetContent(m.renderHistory())
		return m, nil
	}

	// Forward messages to the viewport (e.g., arrow keys, page up/down)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the current state of the model.
func (m RootBrowserModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\nPress 'q' to quit.", m.err))
	}

	if !m.ready {
		return "Initializing terminal..."
	}

	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")
	sb.WriteString(m.renderFooter())

	return sb.String()
}

func (m *RootBrowserModel) renderHistory() string {
	if len(m.history) == 0 && m.isLoading {
		return "Loading history..."
	}
	if len(m.history) == 0 {
		return "No history found."
	}

	var sb strings.Builder
	for i, dto := range m.history {
		roleLabel := strings.ToUpper(dto.Role)
		if dto.Role == "assistant" {
			roleLabel = "MODEL"
		}

		var styledLabel string
		switch dto.Role {
		case "user":
			styledLabel = userStyle.Render(fmt.Sprintf("[%s]", roleLabel))
		case "assistant", "model":
			styledLabel = modelStyle.Render(fmt.Sprintf("[%s]", roleLabel))
		default:
			styledLabel = lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("[%s]", roleLabel))
		}

		if dto.IsArchived {
			styledLabel += archivedStyle.Render(" (archived)")
		}

		sb.WriteString(styledLabel)
		sb.WriteString("\n")

		if dto.ThoughtProcess != "" {
			sb.WriteString(thoughtStyle.Render("[THOUGHTS] " + dto.ThoughtProcess))
			sb.WriteString("\n\n")
		}

		if len(dto.ToolCalls) > 0 {
			for _, tool := range dto.ToolCalls {
				sb.WriteString(toolStyle.Render(fmt.Sprintf("  🔧 Executing tool: %s", tool)))
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}

		sb.WriteString(dto.ContentPreview)
		if i < len(m.history)-1 {
			sb.WriteString("\n\n" + archivedStyle.Render(strings.Repeat("─", m.width/2)) + "\n\n")
		}
	}

	if m.isLoading {
		sb.WriteString("\n\n" + thoughtStyle.Render("Loading more messages..."))
	}

	return sb.String()
}

func (m *RootBrowserModel) renderFooter() string {
	info := footerStyle.Render("↑/↓: Scroll • q: Quit")
	if m.isLoading {
		info += footerStyle.Render(" • LOADING...")
	}
	return info
}

func fetchHistoryCmd(provider ports.UnifiedHistoryProvider, cursor string) tea.Cmd {
	return func() tea.Msg {
		dtos, nextCursor, err := provider.GetHistoryStream(context.Background(), 20, cursor)
		return historyLoadedMsg{
			dtos:       dtos,
			nextCursor: nextCursor,
			err:        err,
		}
	}
}
