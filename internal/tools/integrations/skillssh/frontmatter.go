// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import "strings"

// parseSkillFrontmatter extracts name and description from YAML frontmatter.
// Mirrors the parseSkill logic in internal/infrastructure/skills but is
// intentionally simpler — it only needs name and description for display.
func parseSkillFrontmatter(data []byte) (name, desc string) {
	content := string(data)
	content = strings.ReplaceAll(content, "\r\n", "\n")

	if !strings.HasPrefix(content, "---\n") {
		return "", ""
	}

	parts := strings.SplitN(content, "---\n", 3)
	if len(parts) < 3 {
		return "", ""
	}

	fm := parts[1]
	lines := strings.Split(fm, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}

	return name, desc
}
