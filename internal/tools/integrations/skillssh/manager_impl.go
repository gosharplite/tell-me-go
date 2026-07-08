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
	exec        execRunner
	githubToken string
}

// NewSkillManager creates a new SkillManager with the given dependencies.
func NewSkillManager(skillsShDir string, repo skills.SkillRepository, client tools.HTTPClient, exec execRunner, githubToken string) SkillManager {
	return &defaultSkillManager{
		skillsShDir: skillsShDir,
		repo:        repo,
		client:      client,
		exec:        exec,
		githubToken: githubToken,
	}
}

// searchGitHubAPI queries the GitHub code search API and returns parsed results.
func (m *defaultSkillManager) searchGitHubAPI(ctx context.Context, query string) (*ghSearchResponse, error) {
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
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if m.githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.githubToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}

	var searchResult ghSearchResponse
	if err := json.Unmarshal(body, &searchResult); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &searchResult, nil
}

// skillMeta holds the name and description extracted from a SKILL.md frontmatter.
type skillMeta struct {
	name string
	desc string
}

// fetchSkillMetadataBatch fetches skill name and description concurrently
// from GitHub raw content URLs. It limits concurrency to 4 requests.
// Best-effort: errors from individual fetches are silently ignored.
func fetchSkillMetadataBatch(ctx context.Context, client tools.HTTPClient, items []ghSearchItem) []skillMeta {
	limit := len(items)
	if limit > 10 {
		limit = 10
	}

	metas := make([]skillMeta, limit)
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(4)

	for i := 0; i < limit; i++ {
		i := i
		item := items[i]
		g.Go(func() error {
			name, desc := fetchSkillMeta(gCtx, client, item.Repository.FullName, item.Path, item.Repository.DefaultBranch)
			metas[i] = skillMeta{name: name, desc: desc}
			return nil
		})
	}

	// Best-effort: don't fail the whole search if one fetch times out.
	_ = g.Wait()

	return metas
}

// formatSearchResults builds the human-readable search results string.
func formatSearchResults(items []ghSearchItem, metas []skillMeta, query string) string {
	limit := len(items)
	if limit > 10 {
		limit = 10
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d skills matching %q:\n\n", len(items), query))

	for i, item := range items[:limit] {
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

	return sb.String()
}

// SearchSkills queries the GitHub code search API for skills matching the query.
func (m *defaultSkillManager) SearchSkills(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "Error: query is required and must not be empty.", nil
	}

	searchResult, err := m.searchGitHubAPI(ctx, query)
	if err != nil {
		return "", fmt.Errorf("searching skills: %w", err)
	}

	if len(searchResult.Items) == 0 {
		return fmt.Sprintf("No skills found matching %q.", query), nil
	}

	metas := fetchSkillMetadataBatch(ctx, m.client, searchResult.Items)
	return formatSearchResults(searchResult.Items, metas, query), nil
}

// refreshRepoCache refreshes the skill repository cache if available.
// Best-effort: errors are silently ignored — the mutation already succeeded.
func (m *defaultSkillManager) refreshRepoCache(ctx context.Context) {
	if m.repo != nil {
		_ = m.repo.Refresh(ctx)
	}
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

	m.refreshRepoCache(ctx)

	return fmt.Sprintf("Successfully installed skills from %s/%s to %s\n\n%s\n\nInstalled skills are available immediately. Use `list_skills` to see them.", owner, repoName, targetDir, string(output)), nil
}

// groupSkillsBySource partitions skills into local (docs/skills/) and
// skills.sh (.skills/) groups based on the Source field.
func groupSkillsBySource(all []skills.Skill) (local, ssh []skills.Skill) {
	for _, s := range all {
		switch s.Source {
		case "skills.sh":
			ssh = append(ssh, s)
		default:
			local = append(local, s)
		}
	}
	return local, ssh
}

// formatSkillGroups builds the human-readable installed skills list string.
func formatSkillGroups(local, ssh []skills.Skill) string {
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

	return sb.String()
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

	local, ssh := groupSkillsBySource(all)
	return formatSkillGroups(local, ssh), nil
}

// validateSkillRemovable checks whether a skill can be removed.
// Returns (blockingMessage, nil) if removal is blocked (local skill or not found).
// Returns ("", nil) if removal is allowed.
// Returns ("", error) on repository errors.
func (m *defaultSkillManager) validateSkillRemovable(ctx context.Context, name string) (string, error) {
	if m.repo == nil {
		return "", nil
	}

	all, err := m.repo.GetAll(ctx)
	if err != nil {
		return "", fmt.Errorf("listing skills: %w", err)
	}

	for _, s := range all {
		if s.Name == name {
			if s.Source != "skills.sh" {
				return fmt.Sprintf("Cannot remove %q: it is a local skill (source: %s). Only skills installed from skills.sh (.skills/) can be removed with this tool.", name, s.Source), nil
			}
			return "", nil
		}
	}

	return "", nil
}

// RemoveSkill removes a skills.sh skill from .skills/.
func (m *defaultSkillManager) RemoveSkill(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Error: name is required.", nil
	}

	// Check source — only skills.sh skills can be removed
	if blockMsg, err := m.validateSkillRemovable(ctx, name); err != nil {
		return "", err
	} else if blockMsg != "" {
		return blockMsg, nil
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

	m.refreshRepoCache(ctx)

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
