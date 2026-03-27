// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package prompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	suggesterStyle = lipgloss.NewStyle().
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Background(lipgloss.Color("235"))

	unselectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
)

// suggester renders a list of suggestions and highlights the currently selected one.
type suggester struct {
	Suggestions []string
	Index       int
}

// Update sets the current suggestions and index.
func (s *suggester) Update(suggestions []string, index int) {
	s.Suggestions = suggestions
	s.Index = index
}

// Next cycles to the next suggestion.
func (s *suggester) Next() {
	if len(s.Suggestions) == 0 {
		return
	}
	s.Index = (s.Index + 1) % len(s.Suggestions)
}

// Prev cycles to the previous suggestion.
func (s *suggester) Prev() {
	if len(s.Suggestions) == 0 {
		return
	}
	s.Index = (s.Index - 1 + len(s.Suggestions)) % len(s.Suggestions)
}

// GetSelected returns the currently selected suggestion or empty string.
func (s suggester) GetSelected() string {
	if len(s.Suggestions) == 0 || s.Index < 0 || s.Index >= len(s.Suggestions) {
		return ""
	}
	return s.Suggestions[s.Index]
}

// View renders the suggestion list.
func (s suggester) View() string {
	if len(s.Suggestions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Suggestions:\n")
	for i, suggestion := range s.Suggestions {
		prefix := "  "
		style := unselectedStyle
		if i == s.Index {
			prefix = "> "
			style = selectedStyle
		}
		fmt.Fprintf(&sb, "%s%s\n", prefix, style.Render(suggestion))
	}
	return suggesterStyle.Render(sb.String())
}
