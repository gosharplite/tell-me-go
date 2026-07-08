// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"log/slog"

	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// CompositeRepository aggregates multiple SkillRepository instances,
// merging their results in order. If one source returns an error, it is
// silently skipped so that a single broken source does not block all
// skill selection.
type CompositeRepository struct {
	Repos []domain_skills.SkillRepository
}

// GetAll returns all skills from all contained repositories, concatenated
// in registration order. Errors from individual repositories are logged
// and skipped so that a single broken source does not block skill selection.
func (c *CompositeRepository) GetAll(ctx context.Context) ([]domain_skills.Skill, error) {
	var all []domain_skills.Skill
	for _, r := range c.Repos {
		skills, err := r.GetAll(ctx)
		if err != nil {
			slog.Warn("composite repository: GetAll skipped source", "error", err)
			continue
		}
		all = append(all, skills...)
	}
	return all, nil
}

// Refresh delegates to all contained repositories. Errors from individual
// repositories are logged and skipped so that a single broken source does
// not block cache refresh for other sources.
func (c *CompositeRepository) Refresh(ctx context.Context) error {
	for _, r := range c.Repos {
		if err := r.Refresh(ctx); err != nil {
			slog.Warn("composite repository: Refresh skipped source", "error", err)
			continue
		}
	}
	return nil
}
