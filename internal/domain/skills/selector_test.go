// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"reflect"
	"testing"
)

type mockSkillRepository struct {
	skills []Skill
	err    error
}

func (m *mockSkillRepository) GetAll(ctx context.Context) ([]Skill, error) {
	return m.skills, m.err
}

func setupMockRepo() *mockSkillRepository {
	testSkills := []Skill{
		{
			Name:        "golang-testing",
			Description: "Best practices for testing in Go.",
			Content:     "Some testing content...",
			TokenCount:  100,
		},
		{
			Name:        "golang-patterns",
			Description: "Common Go development patterns.",
			Content:     "Some pattern content...",
			TokenCount:  200,
		},
		{
			Name:        "unrelated-skill",
			Description: "Something else.",
			Content:     "Other content...",
			TokenCount:  50,
		},
	}
	return &mockSkillRepository{skills: testSkills}
}

func TestDefaultSkillSelector_TokenBudgetConstraint(t *testing.T) {
	ctx := context.Background()
	repo := setupMockRepo()

	// Only first skill should fit within the budget of 149
	selector := NewDefaultSkillSelector(repo, 149)
	selected, err := selector.SelectSkills(ctx, "testing stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(selected) != 1 {
		t.Errorf("expected 1 skill, got %d", len(selected))
	}
	if selected[0].Name != "golang-testing" {
		t.Errorf("expected golang-testing, got %s", selected[0].Name)
	}
}

func TestDefaultSkillSelector_KeywordMatchingPrioritization(t *testing.T) {
	ctx := context.Background()
	repo := setupMockRepo()

	// Both testing and pattern would fit within a budget of 400
	selector := NewDefaultSkillSelector(repo, 400)
	selected, err := selector.SelectSkills(ctx, "refactoring patterns and code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// golang-patterns should be first due to "pattern" match
	if len(selected) < 1 || selected[0].Name != "golang-patterns" {
		t.Errorf("expected golang-patterns to be prioritized, got %v", selected)
	}
}

func TestDefaultSkillSelector_ExceedingTokenBudget(t *testing.T) {
	ctx := context.Background()
	repo := setupMockRepo()

	// Even if all match, the budget should limit them
	selector := NewDefaultSkillSelector(repo, 50)
	selected, err := selector.SelectSkills(ctx, "testing patterns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the "unrelated-skill" would fit if it were first, but let's check
	// that the 100-token testing and 200-token patterns are skipped.
	for _, s := range selected {
		if s.TokenCount > 50 {
			t.Errorf("skill %s exceeds budget with token count %d", s.Name, s.TokenCount)
		}
	}
}

func TestDefaultSkillSelector_RelevanceRanking(t *testing.T) {
	ctx := context.Background()
	repo := setupMockRepo()

	selector := NewDefaultSkillSelector(repo, 1000)
	selected, err := selector.SelectSkills(ctx, "tdd is great")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(selected) == 0 || selected[0].Name != "golang-testing" {
		t.Errorf("expected golang-testing to be ranked first for 'tdd', got %v", selected)
	}
}

func TestDefaultSkillSelector_OrderConsistency(t *testing.T) {
	ctx := context.Background()
	testSkills := []Skill{
		{Name: "a", TokenCount: 10},
		{Name: "b", TokenCount: 10},
	}
	repo := &mockSkillRepository{skills: testSkills}
	selector := NewDefaultSkillSelector(repo, 100)

	res1, _ := selector.SelectSkills(ctx, "nothing")
	res2, _ := selector.SelectSkills(ctx, "nothing")

	if !reflect.DeepEqual(res1, res2) {
		t.Error("expected consistent order for same input")
	}
}
