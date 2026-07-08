// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// makeListSkills returns a handler for the list_skills tool.
func makeListSkills(repo skills.SkillRepository) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		if repo == nil {
			return tools.ToolResult{Text: "No skills repository available."}, nil
		}

		all, err := repo.GetAll(ctx)
		if err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error listing skills: %v", err)}, nil
		}

		if len(all) == 0 {
			return tools.ToolResult{Text: "No skills installed.\n\nUse `search_skills <query>` to find installable skills from skills.sh, then `install_skill <repo_url>` to install them."}, nil
		}

		// Group by source
		var local, ssh []skills.Skill
		for _, s := range all {
			switch s.Source {
			case "skills.sh":
				ssh = append(ssh, s)
			default:
				local = append(local, s)
			}
		}

		var sb strings.Builder
		sb.WriteString("Installed skills:\n")

		if len(local) > 0 {
			sb.WriteString("\n  docs/skills/         (local)\n")
			for _, s := range local {
				desc := s.Description
				if desc == "" {
					desc = "(no description)"
				}
				sb.WriteString(fmt.Sprintf("    %-20s %s\n", s.Name, desc))
			}
		}

		if len(ssh) > 0 {
			sb.WriteString("\n  .skills/             (skills.sh)\n")
			for _, s := range ssh {
				desc := s.Description
				if desc == "" {
					desc = "(no description)"
				}
				sb.WriteString(fmt.Sprintf("    %-20s %s\n", s.Name, desc))
			}
		}

		sb.WriteString("\nUse `search_skills <query>` to discover more installable skills.")

		return tools.ToolResult{Text: sb.String()}, nil
	}
}
