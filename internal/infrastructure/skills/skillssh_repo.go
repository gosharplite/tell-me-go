// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// skillsShRepository implements the domain.SkillRepository interface
// by loading skill definitions from SKILL.md files in a .skills/ directory
// (skills.sh format). Unlike fileSkillRepository which reads flat
// docs/skills/<name>/SKILL.md, this reads the nested structure produced
// by git cloning skills.sh repositories:
//
//	$TELL_ME_HOME/.skills/
//	└── owner-repo/
//	    └── skills/
//	        └── skill-name/
//	            └── SKILL.md
//
// parseSkill() and hasSkillName() are shared with fileSkillRepository
// via the same package.
type skillsShRepository struct {
	mu          sync.RWMutex
	skillsShDir string
	cache       []domain.Skill
}

// NewSkillsShRepository creates a new skillsShRepository and immediately
// populates its cache by recursively walking the provided directory for
// SKILL.md files.
//
// If the directory does not exist, an empty repository is returned with
// no error (same pattern as NewFileSkillRepository).
//
// Individual file read/parse errors are logged as warnings and skipped
// (graceful degradation). Only directory traversal errors cause a
// non-nil error return.
func NewSkillsShRepository(skillsShDir string) (domain.SkillRepository, error) {
	repo := &skillsShRepository{skillsShDir: skillsShDir}
	if err := repo.reload(); err != nil {
		return nil, err
	}
	return repo, nil
}

// GetAll returns all cached skill definitions. The returned slice is a
// copy of the internal cache; callers may mutate it safely.
func (r *skillsShRepository) GetAll(ctx context.Context) ([]domain.Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cpy := make([]domain.Skill, len(r.cache))
	copy(cpy, r.cache)
	return cpy, nil
}

// reload re-walks the skillsShDir and replaces the in-memory cache.
// If the directory does not exist, the cache is cleared (empty repository).
// Individual file read/parse errors are logged as warnings and skipped.
func (r *skillsShRepository) reload() error {
	var cache []domain.Skill

	if _, err := os.Stat(r.skillsShDir); os.IsNotExist(err) {
		r.mu.Lock()
		r.cache = cache
		r.mu.Unlock()
		return nil
	}

	err := filepath.Walk(r.skillsShDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Only process files named exactly SKILL.md (skills.sh format).
		// This implicitly skips NOTICE.md and any other files.
		if info.Name() != "SKILL.md" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			slog.Warn("failed to read skill file, skipping",
				"path", path,
				"error", readErr)
			return nil // degrade gracefully
		}

		skill, parseErr := parseSkill(data)
		if parseErr != nil {
			slog.Warn("failed to parse skill file, skipping",
				"path", path,
				"error", parseErr)
			return nil // degrade gracefully
		}
		if skill == nil {
			return nil // skip non-skill files silently
		}

		// Tag as skills.sh source before caching
		skill.Source = "skills.sh"

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
		return fmt.Errorf("load skills from %s: %w", r.skillsShDir, err)
	}

	r.mu.Lock()
	r.cache = cache
	r.mu.Unlock()
	return nil
}

// Refresh re-walks the underlying directory and replaces the cache.
func (r *skillsShRepository) Refresh(ctx context.Context) error {
	return r.reload()
}
