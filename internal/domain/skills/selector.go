// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// DefaultSkillSelector implements the SkillSelector interface with a basic
// keyword-matching heuristic and a token budget constraint.
type DefaultSkillSelector struct {
	repo        SkillRepository
	tokenBudget int
}

// NewDefaultSkillSelector creates a new DefaultSkillSelector.
func NewDefaultSkillSelector(repo SkillRepository, tokenBudget int) *DefaultSkillSelector {
	return &DefaultSkillSelector{
		repo:        repo,
		tokenBudget: tokenBudget,
	}
}

// SelectSkills retrieves all available skills, ranks them by relevance to the
// task description, and returns a subset that fits within the token budget.
func (s *DefaultSkillSelector) SelectSkills(ctx context.Context, taskDescription string) ([]Skill, error) {
	allSkills, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all skills: %w", err)
	}

	// Calculate relevance scores
	type scoredSkill struct {
		skill Skill
		score int
	}

	taskLower := strings.ToLower(taskDescription)
	scored := make([]scoredSkill, 0, len(allSkills))

	for _, skill := range allSkills {
		score := 0

		// Keyword matching heuristic
		if strings.Contains(taskLower, "test") || strings.Contains(taskLower, "tdd") {
			if strings.Contains(strings.ToLower(skill.Name), "testing") {
				score += 10
			}
		}

		if strings.Contains(taskLower, "pattern") || strings.Contains(taskLower, "refactor") {
			if strings.Contains(strings.ToLower(skill.Name), "pattern") {
				score += 10
			}
		}

		// Basic name matching
		skillNameLower := strings.ToLower(skill.Name)
		if strings.Contains(taskLower, skillNameLower) {
			score += 5
		}

		// Check for individual keywords in skill name (e.g., "testing" in "golang-testing")
		keywords := strings.Split(skillNameLower, "-")
		for _, kw := range keywords {
			if len(kw) > 3 && strings.Contains(taskLower, kw) {
				score += 2
			}
		}

		scored = append(scored, scoredSkill{skill: skill, score: score})
	}

	// Sort by score (descending)
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Select skills within token budget
	var selected []Skill
	currentTokens := 0
	for _, ss := range scored {
		if currentTokens+ss.skill.TokenCount <= s.tokenBudget {
			selected = append(selected, ss.skill)
			currentTokens += ss.skill.TokenCount
		}
	}

	return selected, nil
}
