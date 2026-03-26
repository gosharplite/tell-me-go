// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	dashboardStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1)
)

// SessionStats holds the session metrics for display in the dashboard.
type SessionStats struct {
	TurnCount    int
	TokenUsage   int
	ProviderName string
	ModelName    string
}

// Dashboard renders session statistics.
type Dashboard struct {
	Stats SessionStats
}

// Update updates the dashboard state.
func (d *Dashboard) Update(stats SessionStats) {
	d.Stats = stats
}

// View renders the dashboard component.
func (d Dashboard) View() string {
	return dashboardStyle.Render(fmt.Sprintf(
		"Provider: %s | Model: %s | Turns: %d | Tokens: %d",
		d.Stats.ProviderName,
		d.Stats.ModelName,
		d.Stats.TurnCount,
		d.Stats.TokenUsage,
	))
}
