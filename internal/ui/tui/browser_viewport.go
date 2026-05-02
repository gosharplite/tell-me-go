// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func (m *rootBrowserModel) handleViewportUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// Forward messages to the viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	// Infinite pagination trigger (Older history from archive)
	if m.viewport.YOffset == 0 && !m.isLoading && m.cursor != "" && m.cursor != "EOF" && m.ready && !m.isSearching {
		m.isLoading = true
		m.updateViewportContent()
		return m, tea.Batch(append(cmds, fetchHistoryCmd(m.provider, m.cursor))...)
	}

	return m, tea.Batch(cmds...)
}

func (m *rootBrowserModel) updateViewportContent() {
	// 1. Invalidate cache if needed
	if m.lastWidth != m.width || m.lastQuery != m.currentQuery {
		m.cachedThoughts = make(map[string]string)
		m.lastWidth = m.width
		m.lastQuery = m.currentQuery
	}

	// 2. Populate cache for all history turns
	for _, dto := range m.history {
		if _, ok := m.cachedThoughts[dto.ID]; !ok && m.showThoughts && dto.ThoughtProcess != "" {
			m.cachedThoughts[dto.ID] = m.preRenderThought(dto)
		}
	}

	// 3. Render and update state
	rendered, offsets := m.renderHistory()
	m.turnOffsets = offsets
	m.recalculateSearchMatches(rendered)
	m.viewport.SetContent(rendered)
}

func (m *rootBrowserModel) updateViewportHeight() {
	if !m.ready {
		return
	}
	m.viewport.Height = m.height - m.calculateFooterHeight()
}

func (m *rootBrowserModel) preRenderThought(dto ports.HistoryViewDTO) string {
	thoughtText := "💭 [THOUGHTS]\n" + dto.ThoughtProcess
	if m.currentQuery != "" {
		thoughtText = m.highlightMatches(thoughtText, m.currentQuery)
	}

	maxWidth := m.width - 2 // standard prefix "  " or "> "
	if maxWidth < 20 {
		maxWidth = 20
	}

	return thoughtStyle.Width(maxWidth).Render(thoughtText)
}

func (m *rootBrowserModel) renderThoughts(dto ports.HistoryViewDTO, prefix string) string {
	var wrappedText string

	// Pure read from cache
	if cached, ok := m.cachedThoughts[dto.ID]; ok {
		wrappedText = cached
	} else {
		// Computation only, no storage in m.cachedThoughts to keep this pure
		thoughtText := "💭 [THOUGHTS]\n" + dto.ThoughtProcess
		if m.currentQuery != "" {
			thoughtText = m.highlightMatches(thoughtText, m.currentQuery)
		}

		maxWidth := m.width - len(prefix)
		if maxWidth < 20 {
			maxWidth = 20
		}

		wrappedText = thoughtStyle.Width(maxWidth).Render(thoughtText)
	}

	// Apply the dynamic prefix
	var sb strings.Builder
	lines := strings.Split(wrappedText, "\n")
	for _, line := range lines {
		sb.WriteString(prefix)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	return sb.String()
}

func (m *rootBrowserModel) renderEmptyState() string {
	if m.isLoading {
		return "Loading history..."
	}
	if m.cursor == "EOF" {
		return "No history found."
	}
	return ""
}

func (m *rootBrowserModel) renderHistoryStatus(sb *strings.Builder) {
	if m.isLoading {
		sb.WriteString(thoughtStyle.Render("Loading more messages...") + "\n\n")
	} else if m.cursor == "EOF" && len(m.history) > 0 {
		sb.WriteString(archivedStyle.Render("─── Start of History ───") + "\n\n")
	}
}

func (m *rootBrowserModel) renderHistory() (string, []int) {
	if len(m.history) == 0 {
		return m.renderEmptyState(), nil
	}

	turnOffsets := make([]int, 0, len(m.history))
	var sb strings.Builder

	m.renderHistoryStatus(&sb)

	for i, dto := range m.history {
		turnOffsets = append(turnOffsets, strings.Count(sb.String(), "\n"))

		m.renderTurn(&sb, i, dto)

		if i < len(m.history)-1 {
			sb.WriteString(m.renderSeparator())
		}
	}

	return sb.String(), turnOffsets
}

func (m *rootBrowserModel) renderTurn(sb *strings.Builder, i int, dto ports.HistoryViewDTO) {
	prefix := "  "
	if i == m.selectedTurn {
		prefix = "> "
	}

	sb.WriteString(m.renderTurnHeader(dto, i == m.selectedTurn))

	if m.showThoughts && dto.ThoughtProcess != "" {
		sb.WriteString(m.renderThoughts(dto, prefix))
	}

	if len(dto.ToolCalls) > 0 {
		sb.WriteString(m.renderToolCalls(dto, prefix))
	}

	sb.WriteString(m.renderContent(dto, prefix))
}

func (m *rootBrowserModel) renderTurnHeader(dto ports.HistoryViewDTO, isSelected bool) string {
	prefix := "  "
	if isSelected {
		prefix = "> "
	}

	roleLabel := m.getRoleLabel(dto)
	turnStr := m.getTurnLabelSuffix(dto)
	styledLabel := m.getStyledRoleLabel(dto.Role, roleLabel, turnStr)

	if dto.IsArchived {
		styledLabel += archivedStyle.Render(" (archived)")
	}
	if dto.IsPinned {
		styledLabel += lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true).Render(" [PINNED]")
	}

	return prefix + styledLabel + "\n"
}

func (m *rootBrowserModel) getRoleLabel(dto ports.HistoryViewDTO) string {
	roleLabel := strings.ToUpper(dto.Role)
	if dto.Role == "assistant" {
		roleLabel = "MODEL"
	}
	return roleLabel
}

func (m *rootBrowserModel) getTurnLabelSuffix(dto ports.HistoryViewDTO) string {
	if !dto.IsArchived {
		turnIdx := m.getTurnIndex(dto)
		if turnIdx >= 0 {
			return fmt.Sprintf(" - %d", turnIdx+1)
		}
	}
	return ""
}

func (m *rootBrowserModel) getStyledRoleLabel(role, label, suffix string) string {
	switch role {
	case "user":
		return userStyle.Render(fmt.Sprintf("[%s]%s", label, suffix))
	case "assistant", "model":
		return modelStyle.Render(fmt.Sprintf("[%s]%s", label, suffix))
	default:
		return lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("[%s]%s", label, suffix))
	}
}

func (m *rootBrowserModel) renderToolCalls(dto ports.HistoryViewDTO, prefix string) string {
	var sb strings.Builder
	for _, tool := range dto.ToolCalls {
		sb.WriteString(prefix)
		sb.WriteString(toolStyle.Render(fmt.Sprintf("  🔧 Executing tool: %s", tool)))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func (m *rootBrowserModel) renderContent(dto ports.HistoryViewDTO, prefix string) string {
	content := dto.ContentPreview
	if m.currentQuery != "" {
		content = m.highlightMatches(content, m.currentQuery)
	}

	var sb strings.Builder
	lines := strings.Split(content, "\n")
	for j, line := range lines {
		sb.WriteString(prefix)
		sb.WriteString(line)
		if j < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m *rootBrowserModel) renderSeparator() string {
	return "\n\n" + archivedStyle.Render(strings.Repeat("─", m.width/2)) + "\n\n"
}

func (m *rootBrowserModel) renderFooter() string {
	var sb strings.Builder

	activeTurns, pinnedTurns := m.getPinningMetrics()

	if pinnedTurns > 5 || (activeTurns > 0 && float64(pinnedTurns)/float64(activeTurns) > 0.5) {
		sb.WriteString(warningStyle.Render("⚠️ High Pinning Pressure: Auto-summarization may fail."))
		sb.WriteString("\n")
	}

	thoughtsStatus := "ON"
	if !m.showThoughts {
		thoughtsStatus = "OFF"
	}
	fmt.Fprintf(&sb, "↑/↓: Scroll • Space: Thoughts [%s] • /: Search • j/k: Select • p: Pin • r: Rollback • q: Quit", thoughtsStatus)

	if m.currentQuery != "" {
		matchInfo := ""
		if len(m.matches) > 0 {
			matchInfo = fmt.Sprintf(" (%d/%d matches)", m.currentMatch+1, len(m.matches))
		} else {
			matchInfo = " (no matches)"
		}
		fmt.Fprintf(&sb, " • Query: %q%s", m.currentQuery, matchInfo)
	}
	if m.isLoading {
		sb.WriteString(" • LOADING...")
	}
	return footerStyle.Render(sb.String())
}

func (m *rootBrowserModel) calculateFooterHeight() int {
	if m.isSearching {
		return 1
	}

	activeTurns, pinnedTurns := m.getPinningMetrics()

	if pinnedTurns > 5 || (activeTurns > 0 && float64(pinnedTurns)/float64(activeTurns) > 0.5) {
		return 2
	}
	return 1
}
