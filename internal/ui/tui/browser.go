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
	provider     ports.UnifiedHistoryProvider
	history      []ports.HistoryViewDTO
	viewport     viewport.Model
	isLoading    bool
	showThoughts bool
	ready        bool
	cursor       string
	err          error
	width        int
	height       int
}

// NewRootBrowserModel creates a new history browser root model.
func NewRootBrowserModel(provider ports.UnifiedHistoryProvider) *RootBrowserModel {
	return &RootBrowserModel{
		provider:     provider,
		isLoading:    true,
		showThoughts: true,
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
		case " ":
			m.showThoughts = !m.showThoughts
			m.viewport.SetContent(m.renderHistory())
			return m, nil
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
		if len(msg.dtos) > 0 {
			m.history = append(m.history, msg.dtos...)
		}
		m.cursor = msg.nextCursor
		m.viewport.SetContent(m.renderHistory())
		return m, nil
	}

	// Forward messages to the viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	// Infinite pagination trigger
	if m.viewport.AtBottom() && !m.isLoading && m.cursor != "EOF" && m.ready {
		m.isLoading = true
		m.viewport.SetContent(m.renderHistory())
		m.viewport.GotoBottom()
		return m, tea.Batch(append(cmds, fetchHistoryCmd(m.provider, m.cursor))...)
	}

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
	if len(m.history) == 0 && m.cursor == "EOF" {
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

		if m.showThoughts && dto.ThoughtProcess != "" {
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
	} else if m.cursor == "EOF" && len(m.history) > 0 {
		sb.WriteString("\n\n" + archivedStyle.Render("─── End of History ───"))
	}

	return sb.String()
}

func (m *RootBrowserModel) renderFooter() string {
	var sb strings.Builder
	sb.WriteString("↑/↓: Scroll • Space: Toggle Thoughts • q: Quit")
	if m.isLoading {
		sb.WriteString(" • LOADING...")
	}
	if !m.showThoughts {
		sb.WriteString(" (Thoughts hidden)")
	}
	return footerStyle.Render(sb.String())
}

func fetchHistoryCmd(provider ports.UnifiedHistoryProvider, cursor string) tea.Cmd {
	return func() tea.Msg {
		dtos, nextCursor, err := provider.GetHistoryStream(context.Background(), 20, cursor)
		if err == nil && len(dtos) == 0 && cursor != "" {
			nextCursor = "EOF"
		}
		return historyLoadedMsg{
			dtos:       dtos,
			nextCursor: nextCursor,
			err:        err,
		}
	}
}
