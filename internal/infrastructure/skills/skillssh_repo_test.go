// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

func TestNewSkillsShRepository(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantSkills []domain.Skill
		wantErr    bool
		missingDir bool
	}{
		{
			name: "single valid SKILL.md in nested structure",
			files: map[string]string{
				"owner-repo/skills/my-skill/SKILL.md": "---\nname: My Skill\ndescription: My Desc\n---\nMy Content",
			},
			wantSkills: []domain.Skill{
				{
					Name:        "My Skill",
					Description: "My Desc",
					Content:     "My Content",
					TokenCount:  len("My Content") / 4,
					Source:      "skills.sh",
				},
			},
		},
		{
			name:       "missing directory",
			missingDir: true,
			wantSkills: nil,
		},
		{
			name:       "empty directory",
			files:      map[string]string{},
			wantSkills: nil,
		},
		{
			name: "multiple nested directories",
			files: map[string]string{
				"owner-a/repo-a/skills/skill-1/SKILL.md": "---\nname: Skill One\ndescription: Desc One\n---\nContent One",
				"owner-b/repo-b/skills/skill-2/SKILL.md": "---\nname: Skill Two\ndescription: Desc Two\n---\nContent Two",
			},
			wantSkills: []domain.Skill{
				{
					Name:        "Skill One",
					Description: "Desc One",
					Content:     "Content One",
					TokenCount:  len("Content One") / 4,
					Source:      "skills.sh",
				},
				{
					Name:        "Skill Two",
					Description: "Desc Two",
					Content:     "Content Two",
					TokenCount:  len("Content Two") / 4,
					Source:      "skills.sh",
				},
			},
		},
		{
			name: "CRLF line endings",
			files: map[string]string{
				"owner-repo/skills/crlf/SKILL.md": "---\r\nname: CRLF Skill\r\ndescription: CRLF Desc\r\n---\r\nCRLF Content",
			},
			wantSkills: []domain.Skill{
				{
					Name:        "CRLF Skill",
					Description: "CRLF Desc",
					Content:     "CRLF Content",
					TokenCount:  len("CRLF Content") / 4,
					Source:      "skills.sh",
				},
			},
		},
		{
			name: "ignored non-SKILL.md files",
			files: map[string]string{
				"owner-repo/skills/skill-a/NOTICE.md": "---\nname: Ignore Me\ndescription: Ignore\n---\nIgnore",
				"owner-repo/skills/skill-b/README.md": "not a skill",
				"owner-repo/skills/skill-c/skill.md":  "wrong case",
				"owner-repo/other/file.txt":           "not markdown",
				"owner-repo/skills/skill-d/SKILL.md":  "missing frontmatter",
			},
			wantSkills: nil,
		},
		{
			name: "malformed skills skipped gracefully",
			files: map[string]string{
				"owner-repo/skills/no-sep/SKILL.md":  "name: No Separator\ndescription: No Separator\nContent",
				"owner-repo/skills/no-desc/SKILL.md": "---\nname: No Desc\n---\nContent",
				"owner-repo/skills/partial/SKILL.md": "---\nname: Partial\n",
				"owner-repo/skills/no-name/SKILL.md": "---\ndescription: Has desc but no name\n---\nContent here",
			},
			wantSkills: nil, // all skipped gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			skillsDir := tmpDir
			if tt.missingDir {
				skillsDir = filepath.Join(tmpDir, "non-existent")
			} else {
				setupTestFiles(t, tmpDir, tt.files)
			}

			repo, err := NewSkillsShRepository(skillsDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSkillsShRepository() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				gotSkills, _ := repo.GetAll(context.Background())
				assertSkillsMatch(t, gotSkills, tt.wantSkills)
			}
		})
	}
}

func TestSkillsShRepository_GetAll(t *testing.T) {
	tmpDir := t.TempDir()
	skillContent := "---\nname: Test Skill\ndescription: Test Description\n---\nTest Content"
	setupTestFiles(t, tmpDir, map[string]string{
		"owner-repo/skills/test-skill/SKILL.md": skillContent,
	})

	repo, err := NewSkillsShRepository(tmpDir)
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

func TestNewSkillsShRepository_UnreadableFile_SkippedGracefully(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod(0000) does not prevent reads on Windows")
	}

	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "owner-repo", "skills", "bad-skill")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

	badFile := filepath.Join(skillsDir, "SKILL.md")
	if err := os.WriteFile(badFile, []byte("---\nname: Test\ndescription: Test\n---\nContent"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.Chmod(badFile, 0000); err != nil {
		t.Fatalf("failed to chmod file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badFile, 0644) })

	// Unlike fileSkillRepository, skillsShRepository degrades gracefully
	// on individual file errors — skips with warning, no error return.
	repo, err := NewSkillsShRepository(tmpDir)
	if err != nil {
		t.Errorf("expected no error (graceful degradation), got: %v", err)
		return
	}

	skills, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (unreadable file skipped), got %d", len(skills))
	}
}

func TestNewSkillsShRepository_MissingName_SkippedGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "owner-repo", "skills", "no-name")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

	// Valid delimiters, has description, but no name → errInvalidFrontmatter
	if err := os.WriteFile(
		filepath.Join(skillsDir, "SKILL.md"),
		[]byte("---\ndescription: Has desc but no name\n---\nContent here"),
		0644,
	); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Unlike fileSkillRepository which returns an error, skillsShRepository
	// degrades gracefully — skips with warning, no error return.
	repo, err := NewSkillsShRepository(tmpDir)
	if err != nil {
		t.Errorf("expected no error (graceful degradation), got: %v", err)
		return
	}

	skills, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills (invalid frontmatter skipped), got %d", len(skills))
	}
}

func TestNewSkillsShRepository_UnreadableSubdirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod(0000) does not prevent directory reads on Windows")
	}

	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "owner-repo", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

	// Create a subdirectory with no read permission
	badSubdir := filepath.Join(skillsDir, "no_access")
	if err := os.MkdirAll(badSubdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	// Place a valid SKILL.md so the walk actually enters the directory
	if err := os.WriteFile(
		filepath.Join(badSubdir, "SKILL.md"),
		[]byte("---\nname: Valid\ndescription: Valid\n---\nContent"),
		0644,
	); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	if err := os.Chmod(badSubdir, 0000); err != nil {
		t.Fatalf("failed to chmod subdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badSubdir, 0755) })

	// Directory traversal errors still cause the constructor to return an error.
	_, err := NewSkillsShRepository(tmpDir)
	if err == nil || !strings.Contains(err.Error(), "load skills from") {
		t.Errorf("expected 'load skills from' error, got: %v", err)
	}
}

func TestNewSkillsShRepository_DuplicateName(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two SKILL.md files with the same name in different paths
	setupTestFiles(t, tmpDir, map[string]string{
		"owner-a/repo-a/skills/my-skill/SKILL.md": `---
name: my-skill
description: First definition
---
First content.`,
		"owner-b/repo-b/skills/my-skill/SKILL.md": `---
name: my-skill
description: Second definition (should be skipped)
---
Second content.`,
	})

	repo, err := NewSkillsShRepository(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	skills, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(skills) != 1 {
		t.Errorf("expected 1 skill (duplicate skipped), got %d", len(skills))
	}
	if skills[0].Description != "First definition" {
		t.Errorf("expected first-wins, got description %q", skills[0].Description)
	}
}

func TestNewSkillsShRepository_DeeplyNested(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate a deeply nested directory structure
	setupTestFiles(t, tmpDir, map[string]string{
		"org/team/project/skills/deep-skill/SKILL.md": "---\nname: Deep Skill\ndescription: Deeply nested\n---\nDeep content",
	})

	repo, err := NewSkillsShRepository(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	skills, err := repo.GetAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(skills) != 1 {
		t.Errorf("expected 1 skill from deep nesting, got %d", len(skills))
	}
	if skills[0].Name != "Deep Skill" {
		t.Errorf("got name %q, want %q", skills[0].Name, "Deep Skill")
	}
}
