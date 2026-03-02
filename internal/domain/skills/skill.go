// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
)

// Skill represents a specific Go development pattern or testing practice
// that can be injected into the LLM context.
type Skill struct {
	Name        string
	Description string
	Content     string
	TokenCount  int // Heuristic: len(Content) / 4
}

// SkillRepository defines the interface for retrieving available skills.
type SkillRepository interface {
	// GetAll returns all registered skills from the underlying storage.
	GetAll(ctx context.Context) ([]Skill, error)
}

// SkillSelector defines the interface for dynamically selecting relevant
// skills based on a given task description.
type SkillSelector interface {
	// SelectSkills chooses a subset of skills that are most relevant to
	// the provided task description.
	SelectSkills(ctx context.Context, taskDescription string) ([]Skill, error)
}
