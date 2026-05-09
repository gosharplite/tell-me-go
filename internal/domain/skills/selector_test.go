// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

var errRepoFailure = errors.New("repo failure")

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

func assertExpectedSkills(t *testing.T, got []Skill, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d skills, want %d: %+v", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("at index %d: got %s, want %s", i, got[i].Name, name)
		}
	}
}

func TestDefaultSkillSelector_Ordering(t *testing.T) {
	t.Parallel()
	defaultRepo := setupMockRepo()

	tests := []struct {
		name           string
		budget         int
		query          string
		expectedSkills []string
	}{
		{
			name:           "TokenBudgetConstraint",
			budget:         149,
			query:          "testing stuff",
			expectedSkills: []string{"golang-testing"},
		},
		{
			name:           "KeywordMatchingPrioritization",
			budget:         400,
			query:          "refactoring patterns and code",
			expectedSkills: []string{"golang-patterns", "golang-testing", "unrelated-skill"},
		},
		{
			name:           "ExceedingTokenBudget",
			budget:         50,
			query:          "testing patterns",
			expectedSkills: []string{"unrelated-skill"},
		},
		{
			name:           "RelevanceRanking",
			budget:         1000,
			query:          "tdd is great",
			expectedSkills: []string{"golang-testing", "golang-patterns", "unrelated-skill"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			selector := NewDefaultSkillSelector(defaultRepo, tt.budget)
			selected, err := selector.SelectSkills(ctx, tt.query)
			if err != nil {
				t.Fatalf("SelectSkills() unexpected error: %v", err)
			}
			assertExpectedSkills(t, selected, tt.expectedSkills)
		})
	}
}

func TestDefaultSkillSelector_ErrorsAndConsistency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		budget   int
		query    string
		repo     SkillRepository
		validate func(t *testing.T, selector SkillSelector, selected []Skill, err error)
	}{
		{
			name:   "OrderConsistency",
			budget: 100,
			query:  "nothing",
			repo: &mockSkillRepository{
				skills: []Skill{
					{Name: "a", TokenCount: 10},
					{Name: "b", TokenCount: 10},
				},
			},
			validate: func(t *testing.T, selector SkillSelector, selected []Skill, _ error) {
				res2, err := selector.SelectSkills(context.Background(), "nothing")
				if err != nil {
					t.Fatalf("second call failed: %v", err)
				}
				if !reflect.DeepEqual(selected, res2) {
					t.Error("expected consistent order for same input")
				}
			},
		},
		{
			name:   "GetAllErrorPropagation",
			budget: 100,
			query:  "anything",
			repo: &mockSkillRepository{
				err: errRepoFailure,
			},
			validate: func(t *testing.T, _ SkillSelector, selected []Skill, err error) {
				if !errors.Is(err, errRepoFailure) {
					t.Errorf("expected error to wrap errRepoFailure, got: %v", err)
				}
				if selected != nil {
					t.Errorf("expected nil skills slice on error, got: %+v", selected)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			selector := NewDefaultSkillSelector(tt.repo, tt.budget)
			selected, err := selector.SelectSkills(ctx, tt.query)
			tt.validate(t, selector, selected, err)
		})
	}
}
