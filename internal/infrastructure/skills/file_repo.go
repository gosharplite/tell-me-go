// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// errInvalidFrontmatter is returned when a skill file has valid frontmatter
// delimiters but is missing the required name field.
var errInvalidFrontmatter = errors.New("invalid skill frontmatter: name required")

// fileSkillRepository implements the domain.SkillRepository interface
// by loading skill definitions from Markdown files on disk.
type fileSkillRepository struct {
	mu      sync.RWMutex
	docsDir string
	cache   []domain.Skill
}

// isSkillFile returns true if the file entry is a skill Markdown file
// (not a directory, has .md extension, and is not NOTICE.md).
func isSkillFile(info os.FileInfo) bool {
	return !info.IsDir() && filepath.Ext(info.Name()) == ".md" && info.Name() != "NOTICE.md"
}

// hasSkillName returns true if cache already contains a skill with the given name.
func hasSkillName(cache []domain.Skill, name string) bool {
	for _, s := range cache {
		if s.Name == name {
			return true
		}
	}
	return false
}

// NewFileSkillRepository creates a new fileSkillRepository and immediately
// populates its cache by walking the provided directory.
func NewFileSkillRepository(docsDir string) (domain.SkillRepository, error) {
	repo := &fileSkillRepository{docsDir: docsDir}
	if err := repo.reload(); err != nil {
		return nil, err
	}
	return repo, nil
}

// GetAll returns all cached skill definitions.
func (r *fileSkillRepository) GetAll(ctx context.Context) ([]domain.Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cache, nil
}

// reload re-walks the docsDir and replaces the in-memory cache.
// If the directory does not exist, the cache is cleared (empty repository).
// Individual file read/parse errors cause the reload to fail entirely.
func (r *fileSkillRepository) reload() error {
	var cache []domain.Skill

	// Check if directory exists; if not, return empty repository instead of failing.
	// This is important for test environments and first-time setups.
	if _, err := os.Stat(r.docsDir); os.IsNotExist(err) {
		r.mu.Lock()
		r.cache = cache
		r.mu.Unlock()
		return nil
	}

	err := filepath.Walk(r.docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !isSkillFile(info) {
			return nil
		}

		// Parse the skill first so we can check its name before appending.
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read skill file %s: %w", path, readErr)
		}
		skill, parseErr := parseSkill(data)
		if parseErr != nil {
			return fmt.Errorf("parse skill file %s: %w", path, parseErr)
		}
		if skill == nil {
			return nil // skip non-skill files silently
		}
		if hasSkillName(cache, skill.Name) {
			slog.Warn("duplicate skill name detected, skipping",
				"name", skill.Name,
				"path", path)
			return nil
		}
		cache = append(cache, *skill)
		return nil
	})

	if err != nil {
		return fmt.Errorf("load skills from %s: %w", r.docsDir, err)
	}

	r.mu.Lock()
	r.cache = cache
	r.mu.Unlock()
	return nil
}

// Refresh re-walks the underlying directory and replaces the cache.
func (r *fileSkillRepository) Refresh(ctx context.Context) error {
	return r.reload()
}

// validateSkill checks parsed skill fields and reports whether the file
// represents a valid skill. It returns (true, nil) when both name and
// description are present, (false, nil) when the file should be silently
// skipped, and (false, errInvalidFrontmatter) when a valid frontmatter
// block is missing the required name field.
func validateSkill(name, desc string) (bool, error) {
	if name == "" {
		if desc == "" {
			return false, nil
		}
		return false, errInvalidFrontmatter
	}
	if desc == "" {
		return false, nil
	}
	return true, nil
}

// parseSkill extracts the skill metadata from the Markdown frontmatter
// and calculates the token count heuristic.
func parseSkill(data []byte) (*domain.Skill, error) {
	// Normalize Windows line endings to Unix-style for consistent parsing
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	// A skill file must start with "---\n" and have a matching closing "---\n"
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, nil
	}

	parts := bytes.SplitN(data, []byte("---\n"), 3)
	if len(parts) < 3 {
		return nil, nil
	}

	fm := string(parts[1])
	content := strings.TrimSpace(string(parts[2]))

	var name, desc string
	lines := strings.Split(fm, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}

	valid, err := validateSkill(name, desc)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, nil
	}

	return &domain.Skill{
		Name:        name,
		Description: desc,
		Content:     content,
		TokenCount:  len(content) / 4,
		Source:      "local",
	}, nil
}
