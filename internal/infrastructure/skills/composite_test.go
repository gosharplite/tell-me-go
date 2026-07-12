// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"errors"
	"testing"

	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// mockSkillRepo is a test double for domain_skills.SkillRepository.
type mockSkillRepo struct {
	skills []domain_skills.Skill
	err    error
}

func (m *mockSkillRepo) GetAll(ctx context.Context) ([]domain_skills.Skill, error) {
	return m.skills, m.err
}

func (m *mockSkillRepo) Refresh(ctx context.Context) error {
	return m.err
}

func TestCompositeRepository_GetAll(t *testing.T) {
	s1 := domain_skills.Skill{Name: "skill-a", Description: "desc a"}
	s2 := domain_skills.Skill{Name: "skill-b", Description: "desc b"}
	s3 := domain_skills.Skill{Name: "skill-c", Description: "desc c"}
	errBoom := errors.New("boom")

	tests := []struct {
		name    string
		repos   []domain_skills.SkillRepository
		wantLen int
		wantErr bool
	}{
		{
			name:    "nil repos",
			repos:   nil,
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "empty repos",
			repos:   []domain_skills.SkillRepository{},
			wantLen: 0,
			wantErr: false,
		},
		{
			name: "single repo success",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{skills: []domain_skills.Skill{s1, s2}},
			},
			wantLen: 2,
			wantErr: false,
		},
		{
			name: "single repo error",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{err: errBoom},
			},
			wantLen: 0,
			wantErr: false,
		},
		{
			name: "multi repo mixed",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{skills: []domain_skills.Skill{s1}},       // succeeds
				&mockSkillRepo{err: errBoom},                            // errors → skipped
				&mockSkillRepo{skills: []domain_skills.Skill{s2, s3}}, // succeeds
			},
			wantLen: 3,
			wantErr: false,
		},
		{
			name: "all repos error",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{err: errBoom},
				&mockSkillRepo{err: errBoom},
			},
			wantLen: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &CompositeRepository{Repos: tt.repos}
			got, err := cr.GetAll(context.Background())

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("got %d skills, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCompositeRepository_Refresh(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name    string
		repos   []domain_skills.SkillRepository
		wantErr bool
	}{
		{
			name:    "nil repos",
			repos:   nil,
			wantErr: false,
		},
		{
			name:    "empty repos",
			repos:   []domain_skills.SkillRepository{},
			wantErr: false,
		},
		{
			name: "refresh all success",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{},
				&mockSkillRepo{},
			},
			wantErr: false,
		},
		{
			name: "refresh one fails",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{err: errBoom},
				&mockSkillRepo{},
			},
			wantErr: false,
		},
		{
			name: "refresh all fail",
			repos: []domain_skills.SkillRepository{
				&mockSkillRepo{err: errBoom},
				&mockSkillRepo{err: errBoom},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &CompositeRepository{Repos: tt.repos}
			err := cr.Refresh(context.Background())

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
