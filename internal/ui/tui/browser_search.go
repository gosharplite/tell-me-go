// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"regexp"
	"strings"
)

func (m *rootBrowserModel) recalculateSearchMatches(rendered string) {
	m.matches = []int{}
	if m.currentQuery != "" {
		re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(m.currentQuery))
		if err == nil {
			lines := strings.Split(rendered, "\n")
			for i, line := range lines {
				if re.MatchString(line) {
					m.matches = append(m.matches, i)
				}
			}
		}
	}

	if len(m.matches) > 0 {
		if m.currentMatch >= len(m.matches) {
			m.currentMatch = len(m.matches) - 1
		}
	} else {
		m.currentMatch = 0
	}
}

func (m *rootBrowserModel) highlightMatches(text, query string) string {
	if query == "" {
		return text
	}

	// Case-insensitive regex for the query
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return text
	}

	return re.ReplaceAllStringFunc(text, func(match string) string {
		return highlightStyle.Render(match)
	})
}
