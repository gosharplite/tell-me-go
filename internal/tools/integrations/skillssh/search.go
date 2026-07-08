// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ghSearchResponse mirrors the GitHub code search API response.
type ghSearchResponse struct {
	TotalCount int            `json:"total_count"`
	Items      []ghSearchItem `json:"items"`
}

type ghSearchItem struct {
	Name       string        `json:"name"`
	Path       string        `json:"path"`
	Repository ghSearchRepo  `json:"repository"`
}

type ghSearchRepo struct {
	FullName string `json:"full_name"`
}

// ghContentResponse mirrors the GitHub content API response (raw file).
// We don't need to explicitly model it since we fetch raw content.

// makeSearchSkills returns a handler for the search_skills tool.
func makeSearchSkills(client tools.HTTPClient) tools.ToolFunc {
	if client == nil {
		client = http.DefaultClient
	}

	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var params struct {
			Query string `json:"query"`
		}
		if err := tools.UnmarshalArgs(args, &params); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}

		query := strings.TrimSpace(params.Query)
		if query == "" {
			return tools.ToolResult{Text: "Error: query is required and must not be empty."}, nil
		}

		// Build GitHub code search URL
		searchURL := fmt.Sprintf(
			"https://api.github.com/search/code?q=SKILL.md+%s+in:file+path:skills&per_page=10",
			url.QueryEscape(query),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
		if err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error searching skills: %v", err)}, nil
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
		if err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error reading response: %v", err)}, nil
		}

		if resp.StatusCode != http.StatusOK {
			return tools.ToolResult{
				Text: fmt.Sprintf("GitHub API error (status %d): %s", resp.StatusCode, string(body)),
			}, nil
		}

		var searchResult ghSearchResponse
		if err := json.Unmarshal(body, &searchResult); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error parsing response: %v", err)}, nil
		}

		if len(searchResult.Items) == 0 {
			return tools.ToolResult{Text: fmt.Sprintf("No skills found matching %q.", query)}, nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Found %d skills matching %q:\n\n", len(searchResult.Items), query))

		for i, item := range searchResult.Items {
			if i >= 10 {
				break
			}

			// Derive skill name from path: "skills/<name>/SKILL.md" → "<name>"
			skillName := deriveSkillName(item.Path)

			// Try to fetch the SKILL.md content for name/description
			name, desc := fetchSkillMeta(ctx, client, item.Repository.FullName, item.Path)

			if name == "" {
				name = skillName
			}
			if desc == "" {
				desc = "(no description)"
			}

			sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, name))
			sb.WriteString(fmt.Sprintf("   Description: %s\n", desc))
			sb.WriteString(fmt.Sprintf("   Repository: %s\n", item.Repository.FullName))
			sb.WriteString(fmt.Sprintf("   Install: `install_skill https://github.com/%s`\n\n", item.Repository.FullName))
		}

		return tools.ToolResult{Text: sb.String()}, nil
	}
}

// deriveSkillName extracts the skill directory name from a SKILL.md path.
// "skills/mcp-builder/SKILL.md" → "mcp-builder"
func deriveSkillName(path string) string {
	parts := strings.Split(path, "/")
	// Expect: .../skills/<name>/SKILL.md
	for i, p := range parts {
		if p == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	// Fallback: use the parent directory of SKILL.md
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "SKILL.md" && i > 0 {
			return parts[i-1]
		}
	}
	return path
}

// fetchSkillMeta fetches a raw SKILL.md from GitHub and extracts
// the name and description from YAML frontmatter.
func fetchSkillMeta(ctx context.Context, client tools.HTTPClient, repoFullName, path string) (name, desc string) {
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/%s", repoFullName, path)

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
