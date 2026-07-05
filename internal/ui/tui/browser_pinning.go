// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

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

func (m *rootBrowserModel) syncViewportToSelectedTurn() {
	if m.selectedTurn >= 0 && m.selectedTurn < len(m.turnOffsets) {
		targetLine := m.turnOffsets[m.selectedTurn]
		if targetLine < m.viewport.YOffset {
			m.viewport.SetYOffset(targetLine)
		}
		if targetLine >= m.viewport.YOffset+m.viewport.Height {
			m.viewport.SetYOffset(targetLine - m.viewport.Height + 1)
		}
	}
}

func (m *rootBrowserModel) getSystemOffset() int {
	for _, dto := range m.history {
		if dto.OriginalIndex == 0 && dto.Role == "system" {
			return 1
		}
	}
	return 0
}

// truncateID returns a short prefix of a UUID for display in error messages.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// getTurnStartOriginalIndex returns the index of the first message in a turn (usually the 'user' msg).
func (m *rootBrowserModel) getTurnStartOriginalIndex(dto ports.HistoryViewDTO) int {
	offset := m.getSystemOffset()
	msgOffset := dto.OriginalIndex - offset
	if msgOffset < 0 {
		return dto.OriginalIndex
	}
	return (msgOffset & ^1) + offset
}

func (m *rootBrowserModel) getPinningMetrics() (activeTurns int, pinnedTurns int) {
	seen := make(map[string]bool)
	for _, dto := range m.history {
		if dto.IsArchived || dto.ID == "" {
			continue
		}
		if seen[dto.ID] {
			continue
		}
		seen[dto.ID] = true
		activeTurns++
		if dto.IsPinned {
			pinnedTurns++
		}
	}
	return activeTurns, pinnedTurns
}

func (m *rootBrowserModel) getTurnForPinning() (ports.HistoryViewDTO, bool) {
	if m.selectedTurn == -1 || m.selectedTurn >= len(m.history) {
		return ports.HistoryViewDTO{}, false
	}

	dto := m.history[m.selectedTurn]
	if dto.IsArchived {
		return ports.HistoryViewDTO{}, false
	}

	if dto.ID == "" {
		return ports.HistoryViewDTO{}, false
	}
	return dto, true
}

func (m *rootBrowserModel) togglePin() {
	dto, ok := m.getTurnForPinning()
	if !ok {
		return
	}

	err := m.cmdService.SetPinned(context.Background(), dto.ID, !dto.IsPinned)
	if err != nil {
		m.err = err
		return
	}

	updated := m.updateLocalPinState(dto, !dto.IsPinned)
	if !updated {
		// Local state couldn't be updated — full refresh needed.
		m.history = nil
		m.cursor = ""
		m.selectedTurn = -1
		m.isLoading = true
		m.lastMutationTime = time.Now()
		return
	}

	m.lastMutationTime = time.Now()
	m.updateViewportContent()
	m.updateViewportHeight()
}

func (m *rootBrowserModel) updateLocalPinState(dto ports.HistoryViewDTO, newPinState bool) bool {
	turnStartIdx := m.getTurnStartOriginalIndex(dto)
	updated := false
	for idx := range m.history {
		if !m.history[idx].IsArchived && m.getTurnStartOriginalIndex(m.history[idx]) == turnStartIdx {
			m.history[idx].IsPinned = newPinState
			updated = true
		}
	}
	return updated
}

func (m *rootBrowserModel) rollbackToSelected() tea.Cmd {
	if m.selectedTurn == -1 || m.selectedTurn >= len(m.history) {
		return nil
	}

	dto := m.history[m.selectedTurn]
	if dto.IsArchived {
		m.err = fmt.Errorf("cannot rollback: turn %s is archived and read-only", truncateID(dto.ID))
		return nil
	}

	// We need total active messages to calculate how many turns to remove.
	lastActiveIdx := -1
	for i := len(m.history) - 1; i >= 0; i-- {
		if !m.history[i].IsArchived {
			lastActiveIdx = m.history[i].OriginalIndex
			break
		}
	}
	if lastActiveIdx == -1 {
		return nil
	}

	targetStartIdx := m.getTurnStartOriginalIndex(dto)
	turnsToRemove := (lastActiveIdx - targetStartIdx + 1 + 1) / 2

	_, _, _, err := m.cmdService.RollbackTurns(context.Background(), turnsToRemove)
	if err != nil {
		m.err = err
		return nil
	}

	m.lastMutationTime = time.Now()
	// Full Refresh
	m.history = nil
	m.cursor = ""
	m.selectedTurn = -1
	m.isLoading = true
	return tea.Batch(
		fetchHistoryCmd(m.provider, ""),
		refreshTimeoutCmd(30*time.Second),
	)
}

func refreshTimeoutCmd(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return historyLoadedMsg{err: fmt.Errorf("refresh timed out after %v", d)}
	}
}
