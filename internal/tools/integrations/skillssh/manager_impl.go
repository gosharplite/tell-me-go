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
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/sync/errgroup"
)

// ghRepoURL matches GitHub repository URLs: https://github.com/<owner>/<repo>
// Optional trailing slash and .git suffix are stripped during normalization.
var ghRepoURL = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`)

// defaultSkillManager is the concrete implementation of SkillManager.
type defaultSkillManager struct {
	skillsShDir string
	repo        skills.SkillRepository
	client      tools.HTTPClient
	exec        ExecRunner
	githubToken string
}

// NewSkillManager creates a new SkillManager with the given dependencies.
func NewSkillManager(skillsShDir string, repo skills.SkillRepository, client tools.HTTPClient, exec ExecRunner, githubToken string) SkillManager {
	return &defaultSkillManager{
		skillsShDir: skillsShDir,
		repo:        repo,
		client:      client,
		exec:        exec,
		githubToken: githubToken,
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
		return "", fmt.Errorf("searching skills: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if m.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.githubToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("searching skills: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("GitHub API error (status %d): %s", resp.StatusCode, string(body)), nil
	}

	var searchResult ghSearchResponse
	if err := json.Unmarshal(body, &searchResult); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(searchResult.Items) == 0 {
		return fmt.Sprintf("No skills found matching %q.", query), nil
	}

	// Fetch skill metadata concurrently (max 4 parallel HTTP requests).
	type skillMeta struct {
		name string
		desc string
	}

	limit := len(searchResult.Items)
	if limit > 10 {
		limit = 10
	}

	metas := make([]skillMeta, limit)
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	for i := 0; i < limit; i++ {
		i := i
		item := searchResult.Items[i]
		g.Go(func() error {
			name, desc := fetchSkillMeta(gCtx, client, item.Repository.FullName, item.Path, item.Repository.DefaultBranch)
			metas[i] = skillMeta{name: name, desc: desc}
			return nil
		})
	}

	// Best-effort: don't fail the whole search if one fetch times out.
	_ = g.Wait()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d skills matching %q:\n\n", len(searchResult.Items), query))

	for i, item := range searchResult.Items[:limit] {
		meta := metas[i]
		skillName := deriveSkillName(item.Path)
		name, desc := meta.name, meta.desc

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
		return "", fmt.Errorf("creating .skills/ directory: %w", err)
	}

	if m.exec == nil {
		return "Error: command execution is not available.", nil
	}

	output, err := m.exec(ctx, "git", "clone", "--depth", "1", "--single-branch", repoURL, targetDir)
	if err != nil {
		return "", fmt.Errorf("cloning repository: %w\n%s", err, string(output))
	}

	// Refresh the repository cache so the newly installed skill is visible immediately.
	if m.repo != nil {
		if err := m.repo.Refresh(ctx); err != nil {
			// Log but don't fail — the mutation succeeded; cache refresh is best-effort.
			// The skill data will be picked up on the next refresh regardless.
		}
	}

	return fmt.Sprintf("Successfully installed skills from %s/%s to %s\n\n%s\n\nInstalled skills are available immediately. Use `list_skills` to see them.", owner, repoName, targetDir, string(output)), nil
}

// ListSkills returns a formatted list of all installed skills grouped by source.
func (m *defaultSkillManager) ListSkills(ctx context.Context) (string, error) {
	if m.repo == nil {
		return "No skills repository available.", nil
	}

	all, err := m.repo.GetAll(ctx)
	if err != nil {
		return "", fmt.Errorf("listing skills: %w", err)
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
		return "", fmt.Errorf("searching for skill: %w", err)
	}

	if !found {
		return fmt.Sprintf("Skill %q not found in .skills/. Use `list_skills` to see installed skills.", name), nil
	}

	// Find the repo root: walk up from the SKILL.md until we hit a directory
	// whose parent is skillsShDir. That's the cloned repo directory.
	repoRoot := findRepoRoot(m.skillsShDir, skillDir)

	if err := os.RemoveAll(repoRoot); err != nil {
		return "", fmt.Errorf("removing skill repository %s: %w", repoRoot, err)
	}

	// Refresh the repository cache so the removal is visible immediately.
	if m.repo != nil {
		if err := m.repo.Refresh(ctx); err != nil {
			// Log but don't fail — the mutation succeeded; cache refresh is best-effort.
			// The skill data will be picked up on the next refresh regardless.
		}
	}

	return fmt.Sprintf("Successfully removed skill %q (repository %s).", name, repoRoot), nil
}

// findSkillDir walks skillsShDir looking for a SKILL.md whose frontmatter
// name matches the given skill name. Returns the parent directory of the
// matching SKILL.md.
func findSkillDir(skillsShDir, skillName string) (found bool, dir string, err error) {
	if _, statErr := os.Stat(skillsShDir); os.IsNotExist(statErr) {
		return false, "", nil
	}

	err = filepath.Walk(skillsShDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() != "SKILL.md" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // skip unreadable files
		}

		name, _ := parseSkillFrontmatter(data)
		if name == skillName {
			found = true
			dir = filepath.Dir(path)
			return filepath.SkipAll // stop walking
		}
		return nil
	})

	return found, dir, err
}

// findRepoRoot walks up from skillDir until it reaches a directory whose
// parent is skillsShDir. That directory is the cloned repo root. Removing
// at repo granularity ensures that reinstalling the repo after removing a
// single skill works correctly.
func findRepoRoot(skillsShDir, skillDir string) string {
	for dir := skillDir; dir != skillsShDir && dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if filepath.Dir(dir) == skillsShDir {
			return dir
		}
	}
	return skillDir // fallback: shouldn't happen, but safe
}
