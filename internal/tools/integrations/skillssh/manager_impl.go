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
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// defaultSkillManager is the concrete implementation of SkillManager.
type defaultSkillManager struct {
	skillsShDir string
	repo        skills.SkillRepository
	client      tools.HTTPClient
	exec        ExecRunner
}

// NewSkillManager creates a new SkillManager with the given dependencies.
func NewSkillManager(skillsShDir string, repo skills.SkillRepository, client tools.HTTPClient, exec ExecRunner) SkillManager {
	return &defaultSkillManager{
		skillsShDir: skillsShDir,
		repo:        repo,
		client:      client,
		exec:        exec,
	}
}

// SearchSkills queries the GitHub code search API for skills matching the query.
func (m *defaultSkillManager) SearchSkills(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "Error: query is required and must not be empty.", nil
	}

	client := m.client
	if client == nil {
		client = http.DefaultClient
	}

	searchURL := fmt.Sprintf(
		"https://api.github.com/search/code?q=SKILL.md+%s+in:file+path:skills&per_page=10",
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error searching skills: %v", err), err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return fmt.Sprintf("Error reading response: %v", err), err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("GitHub API error (status %d): %s", resp.StatusCode, string(body)), nil
	}

	var searchResult ghSearchResponse
	if err := json.Unmarshal(body, &searchResult); err != nil {
		return fmt.Sprintf("Error parsing response: %v", err), err
	}

	if len(searchResult.Items) == 0 {
		return fmt.Sprintf("No skills found matching %q.", query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d skills matching %q:\n\n", len(searchResult.Items), query))

	for i, item := range searchResult.Items {
		if i >= 10 {
			break
		}

		skillName := deriveSkillName(item.Path)
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

	return sb.String(), nil
}

// InstallSkill clones a GitHub skills repository into .skills/.
func (m *defaultSkillManager) InstallSkill(ctx context.Context, repoURL string) (string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "Error: repo_url is required.", nil
	}

	matches := ghRepoURL.FindStringSubmatch(repoURL)
	if matches == nil {
		return fmt.Sprintf("Error: invalid GitHub repository URL: %s\n\nURL must be in the format: https://github.com/<owner>/<repo>", repoURL), nil
	}

	owner := matches[1]
	repoName := matches[2]

	targetDir := filepath.Join(m.skillsShDir, fmt.Sprintf("%s-%s", owner, repoName))

	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Sprintf("Skill repository %s/%s is already installed at %s.\n\nUse `list_skills` to see installed skills or `remove_skill` to remove it first.", owner, repoName, targetDir), nil
	}

	if err := os.MkdirAll(m.skillsShDir, 0755); err != nil {
		return fmt.Sprintf("Error creating .skills/ directory: %v", err), err
	}

	if m.exec == nil {
		return "Error: command execution is not available.", nil
	}

	output, err := m.exec(ctx, "git", "clone", repoURL, targetDir)
	if err != nil {
		return fmt.Sprintf("Error cloning repository:\n%s\n\nError: %v", string(output), err), err
	}

	// Refresh the repository cache so the newly installed skill is visible immediately.
	if m.repo != nil {
		_ = m.repo.Refresh(ctx)
	}

	return fmt.Sprintf("Successfully installed skills from %s/%s to %s\n\n%s\n\nInstalled skills will be available on the next message. Use `list_skills` to see them.", owner, repoName, targetDir, string(output)), nil
}

// ListSkills returns a formatted list of all installed skills grouped by source.
func (m *defaultSkillManager) ListSkills(ctx context.Context) (string, error) {
	if m.repo == nil {
		return "No skills repository available.", nil
	}

	all, err := m.repo.GetAll(ctx)
	if err != nil {
		return fmt.Sprintf("Error listing skills: %v", err), err
	}

	if len(all) == 0 {
		return "No skills installed.\n\nUse `search_skills <query>` to find installable skills from skills.sh, then `install_skill <repo_url>` to install them.", nil
	}

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

	return sb.String(), nil
}

// RemoveSkill removes a skills.sh skill from .skills/.
func (m *defaultSkillManager) RemoveSkill(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Error: name is required.", nil
	}

	// Check source — only skills.sh skills can be removed
	if m.repo != nil {
		all, err := m.repo.GetAll(ctx)
		if err == nil {
			for _, s := range all {
				if s.Name == name {
					if s.Source != "skills.sh" {
						return fmt.Sprintf("Cannot remove %q: it is a local skill (source: %s). Only skills installed from skills.sh (.skills/) can be removed with this tool.", name, s.Source), nil
					}
					break
				}
			}
		}
	}

	found, skillDir, err := findSkillDir(m.skillsShDir, name)
	if err != nil {
		return fmt.Sprintf("Error searching for skill: %v", err), err
	}

	if !found {
		return fmt.Sprintf("Skill %q not found in .skills/. Use `list_skills` to see installed skills.", name), nil
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Sprintf("Error removing skill directory %s: %v", skillDir, err), err
	}

	// Refresh the repository cache so the removal is visible immediately.
	if m.repo != nil {
		_ = m.repo.Refresh(ctx)
	}

	return fmt.Sprintf("Successfully removed skill %q from %s.", name, skillDir), nil
}
