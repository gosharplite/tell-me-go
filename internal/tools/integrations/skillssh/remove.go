// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// makeRemoveSkill returns a handler for the remove_skill tool.
func makeRemoveSkill(mgr SkillManager) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var params struct {
			Name string `json:"name"`
		}
		if err := tools.UnmarshalArgs(args, &params); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}

		output, err := mgr.RemoveSkill(ctx, params.Name)
		if err != nil {
			return tools.ToolResult{Error: err, Text: output}, nil
		}
		return tools.ToolResult{Text: output}, nil
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

// findRepoRoot walks up from skillDir until it reaches a directory whose
// parent is skillsShDir. That directory is the cloned repo root. Removing
// at repo granularity ensures that reinstalling the repo after removing a
// single skill works correctly.
func findRepoRoot(skillsShDir, skillDir string) string {
	for dir := skillDir; dir != skillsShDir && dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if filepath.Dir(dir) == skillsShDir {
			return dir
		}
	}
	return skillDir // fallback: shouldn't happen, but safe
}
