// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ghSearchResponse mirrors the GitHub code search API response.
type ghSearchResponse struct {
	TotalCount int            `json:"total_count"`
	Items      []ghSearchItem `json:"items"`
}

type ghSearchItem struct {
	Name       string       `json:"name"`
	Path       string       `json:"path"`
	Repository ghSearchRepo `json:"repository"`
}

type ghSearchRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

// makeSearchSkills returns a handler for the search_skills tool.
func makeSearchSkills(mgr SkillManager) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var params struct {
			Query string `json:"query"`
		}
		if err := tools.UnmarshalArgs(args, &params); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}

		output, err := mgr.SearchSkills(ctx, params.Query)
		if err != nil {
			return tools.ToolResult{Error: err, Text: output}, nil
		}
		return tools.ToolResult{Text: output}, nil
	}
}

// deriveSkillName extracts the skill directory name from a SKILL.md path.
// "skills/mcp-builder/SKILL.md" → "mcp-builder"
func deriveSkillName(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "SKILL.md" && i > 0 {
			return parts[i-1]
		}
	}
	return path
}

// fetchSkillMeta fetches a raw SKILL.md from GitHub and extracts
// the name and description from YAML frontmatter. The branch parameter
// controls which branch is fetched; if empty, "main" is used as default.
func fetchSkillMeta(ctx context.Context, client tools.HTTPClient, repoFullName, path, branch string) (name, desc string) {
	if branch == "" {
		branch = "main"
	}
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repoFullName, branch, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", ""
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<17)) // 128KB limit
	if err != nil {
		return "", ""
	}

	return parseSkillFrontmatter(body)
}

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
