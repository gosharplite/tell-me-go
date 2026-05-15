// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// ErrInvalidFrontmatter is returned when a skill file has valid frontmatter
// delimiters but is missing the required name field.
var ErrInvalidFrontmatter = errors.New("invalid skill frontmatter: name required")

// fileSkillRepository implements the domain.SkillRepository interface
// by loading skill definitions from Markdown files on disk.
type fileSkillRepository struct {
	cache []domain.Skill
}

// isSkillFile returns true if the file entry is a skill Markdown file
// (not a directory, has .md extension, and is not NOTICE.md).
func isSkillFile(info os.FileInfo) bool {
	return !info.IsDir() && filepath.Ext(info.Name()) == ".md" && info.Name() != "NOTICE.md"
}

// loadSkillFile reads and parses a single skill file, appending the result to cache.
func loadSkillFile(path string, cache *[]domain.Skill) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read skill file %s: %w", path, err)
	}

	skill, err := parseSkill(data)
	if err != nil {
		return fmt.Errorf("parse skill file %s: %w", path, err)
	}

	if skill != nil {
		*cache = append(*cache, *skill)
	}

	return nil
}

// NewFileSkillRepository creates a new fileSkillRepository and immediately
// populates its cache by walking the provided directory.
func NewFileSkillRepository(docsDir string) (domain.SkillRepository, error) {
	var cache []domain.Skill

	// Check if directory exists; if not, return empty repository instead of failing.
	// This is important for test environments and first-time setups.
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		return &fileSkillRepository{cache: cache}, nil
	}

	err := filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !isSkillFile(info) {
			return nil
		}

		return loadSkillFile(path, &cache)
	})

	if err != nil {
		return nil, fmt.Errorf("load skills from %s: %w", docsDir, err)
	}

	return &fileSkillRepository{cache: cache}, nil
}

// GetAll returns all cached skill definitions.
func (r *fileSkillRepository) GetAll(ctx context.Context) ([]domain.Skill, error) {
	return r.cache, nil
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

	// Only return a Skill if it has both name and description.
	// If the file had valid frontmatter delimiters (opening and closing "---")
	// but is missing the required name field, treat it as a parse error.
	if name == "" && desc == "" {
		return nil, nil
	}
	if name == "" {
		return nil, ErrInvalidFrontmatter
	}
	if desc == "" {
		return nil, nil
	}

	return &domain.Skill{
		Name:        name,
		Description: desc,
		Content:     content,
		TokenCount:  len(content) / 4,
	}, nil
}
