// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// FileSkillRepository implements the domain.SkillRepository interface
// by loading skill definitions from Markdown files on disk.
type FileSkillRepository struct {
	cache []domain.Skill
}

// NewFileSkillRepository creates a new FileSkillRepository and immediately
// populates its cache by walking the provided directory.
func NewFileSkillRepository(docsDir string) (*FileSkillRepository, error) {
	var cache []domain.Skill

	// Check if directory exists; if not, return empty repository instead of failing.
	// This is important for test environments and first-time setups.
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		return &FileSkillRepository{cache: cache}, nil
	}

	err := filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}

		// Skip NOTICE.md or other non-skill files
		if info.Name() == "NOTICE.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read skill file %s: %w", path, err)
		}

		skill, err := parseSkill(data)
		if err != nil {
			return fmt.Errorf("parse skill file %s: %w", path, err)
		}

		if skill != nil {
			cache = append(cache, *skill)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("load skills from %s: %w", docsDir, err)
	}

	return &FileSkillRepository{cache: cache}, nil
}

// GetAll returns all cached skill definitions.
func (r *FileSkillRepository) GetAll(ctx context.Context) ([]domain.Skill, error) {
	return r.cache, nil
}

// parseSkill extracts the skill metadata from the Markdown frontmatter
// and calculates the token count heuristic.
func parseSkill(data []byte) (*domain.Skill, error) {
	// A skill file must start with "---" and have a matching closing "---"
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

	// Only return a Skill if it has both name and description
	if name == "" || desc == "" {
		return nil, nil
	}

	return &domain.Skill{
		Name:        name,
		Description: desc,
		Content:     content,
		TokenCount:  len(content) / 4,
	}, nil
}
