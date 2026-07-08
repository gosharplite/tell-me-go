// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// makeRemoveSkill returns a handler for the remove_skill tool.
func makeRemoveSkill(skillsShDir string, repo skills.SkillRepository) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var params struct {
			Name string `json:"name"`
		}
		if err := tools.UnmarshalArgs(args, &params); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}

		name := params.Name
		if name == "" {
			return tools.ToolResult{Text: "Error: name is required."}, nil
		}

		// First, check if the skill exists and its source
		if repo != nil {
			all, err := repo.GetAll(ctx)
			if err == nil {
				for _, s := range all {
					if s.Name == name {
						if s.Source != "skills.sh" {
							return tools.ToolResult{
								Text: fmt.Sprintf("Cannot remove %q: it is a local skill (source: %s). Only skills installed from skills.sh (.skills/) can be removed with this tool.", name, s.Source),
							}, nil
						}
						break
					}
				}
			}
		}

		// Walk .skills/ to find the SKILL.md with matching name and remove its parent directory
		found, skillDir, err := findSkillDir(skillsShDir, name)
		if err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error searching for skill: %v", err)}, nil
		}

		if !found {
			return tools.ToolResult{Text: fmt.Sprintf("Skill %q not found in .skills/. Use `list_skills` to see installed skills.", name)}, nil
		}

		if err := os.RemoveAll(skillDir); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error removing skill directory %s: %v", skillDir, err)}, nil
		}

		return tools.ToolResult{
			Text: fmt.Sprintf("Successfully removed skill %q from %s.", name, skillDir),
		}, nil
	}
}

// findSkillDir walks skillsShDir looking for a SKILL.md whose frontmatter
// name matches the given skill name. Returns the parent directory of the
// matching SKILL.md.
func findSkillDir(skillsShDir, skillName string) (found bool, dir string, err error) {
	if _, statErr := os.Stat(skillsShDir); os.IsNotExist(statErr) {
		return false, "", nil
	}

	err = filepath.Walk(skillsShDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() != "SKILL.md" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // skip unreadable files
		}

		name, _ := parseSkillFrontmatter(data)
		if name == skillName {
			found = true
			dir = filepath.Dir(path)
			return filepath.SkipAll // stop walking
		}
		return nil
	})

	return found, dir, err
}
