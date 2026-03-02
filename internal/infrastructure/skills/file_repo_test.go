// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

func TestNewFileSkillRepository(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string
		wantSkills    []domain.Skill
		wantErr       bool
		missingDir    bool
	}{
		{
			name: "valid skills",
			files: map[string]string{
				"skill1.md": "---\nname: Skill One\ndescription: Desc One\n---\nContent One",
				"skill2.md": "---\nname: Skill Two\ndescription: Desc Two\n---\nContent Two",
			},
			wantSkills: []domain.Skill{
				{
					Name:        "Skill One",
					Description: "Desc One",
					Content:     "Content One",
					TokenCount:  len("Content One") / 4,
				},
				{
					Name:        "Skill Two",
					Description: "Desc Two",
					Content:     "Content Two",
					TokenCount:  len("Content Two") / 4,
				},
			},
		},
		{
			name:       "missing directory",
			missingDir: true,
			wantSkills: nil,
		},
		{
			name: "ignored files",
			files: map[string]string{
				"NOTICE.md":   "---\nname: Ignore Me\ndescription: Ignore Me\n---\nIgnore",
				"readme.txt":  "not markdown",
				"nested/dir":  "should be ignored",
				"other.md":    "missing frontmatter",
			},
			wantSkills: nil,
		},
		{
			name: "malformed skills",
			files: map[string]string{
				"no_sep.md":   "name: No Separator\ndescription: No Separator\nContent",
				"no_name.md":  "---\ndescription: No Name\n---\nContent",
				"no_desc.md":  "---\nname: No Desc\n---\nContent",
				"partial.md":  "---\nname: Partial\n",
			},
			wantSkills: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			docsDir := tmpDir
			if tt.missingDir {
				docsDir = filepath.Join(tmpDir, "non-existent")
			} else {
				setupTestFiles(t, tmpDir, tt.files)
			}

			repo, err := NewFileSkillRepository(docsDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFileSkillRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				gotSkills, _ := repo.GetAll(context.Background())
				assertSkillsMatch(t, gotSkills, tt.wantSkills)
			}
		})
	}
}

func setupTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if filepath.Ext(name) == "" && !os.IsPathSeparator(name[len(name)-1]) {
			// Create a directory if it looks like one
			err := os.MkdirAll(path, 0755)
			if err != nil {
				t.Fatalf("failed to create directory: %v", err)
			}
			continue
		}
		err := os.MkdirAll(filepath.Dir(path), 0755)
		if err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		err = os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}
}

func assertSkillsMatch(t *testing.T, got []domain.Skill, want []domain.Skill) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %d skills, want %d", len(got), len(want))
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if reflect.DeepEqual(g, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("did not find expected skill: %+v", w)
		}
	}
}

func TestFileSkillRepository_GetAll(t *testing.T) {
	tmpDir := t.TempDir()
	skillContent := "---\nname: Test Skill\ndescription: Test Description\n---\nTest Content"
	err := os.WriteFile(filepath.Join(tmpDir, "test.md"), []byte(skillContent), 0644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	repo, err := NewFileSkillRepository(tmpDir)
	if err != nil {
		t.Fatalf("failed to create repository: %v", err)
	}

	skills, err := repo.GetAll(context.Background())
	if err != nil {
		t.Errorf("GetAll() error = %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("got %d skills, want 1", len(skills))
	} else {
		if skills[0].Name != "Test Skill" {
			t.Errorf("got name %q, want %q", skills[0].Name, "Test Skill")
		}
	}
}
