// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ExecRunner executes a command and returns its combined output.
// Implementations should use exec.CommandContext or equivalent.
type ExecRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// RegisterSkillsShTools registers all skills.sh tools into the provided registry.
//
// Parameters:
//   - r: the tool registry to register with
//   - skillsShDir: path to .skills/ directory (for install/remove operations)
//   - repo: the skill repository (for list_skills)
//   - client: HTTP client for search_skills GitHub API calls
//   - exec: function to run shell commands (for git clone)
func RegisterSkillsShTools(r tools.Registry, skillsShDir string, repo skills.SkillRepository, client tools.HTTPClient, exec ExecRunner) error {
	if r == nil {
		return fmt.Errorf("registry is required")
	}

	reg := func(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
		return r.RegisterToToolkit("skillssh", def, handler)
	}
	regConsent := func(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
		return r.RegisterToToolkitWithOptions("skillssh", def, handler, tools.ToolOptions{Serial: true})
	}

	// search_skills — read-only
	if err := reg(&tools.ToolDeclaration{
		Name:        "search_skills",
		Description: "Search the skills.sh ecosystem for installable skills matching a query. Returns skill names, descriptions, and the install command to use.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"query": {
					Type:        "STRING",
					Description: "Search term for skills (e.g., 'kubernetes', 'sql', 'react').",
				},
			},
			Required: []string{"query"},
		},
	}, makeSearchSkills(client)); err != nil {
		return err
	}

	// list_skills — read-only
	if err := reg(&tools.ToolDeclaration{
		Name:        "list_skills",
		Description: "List all installed skills from both local (docs/skills/) and skills.sh (.skills/) sources.",
		Parameters: &tools.Schema{
			Type:       "OBJECT",
			Properties: map[string]*tools.Schema{},
		},
	}, makeListSkills(repo)); err != nil {
		return err
	}

	// install_skill — write, requires consent
	if err := regConsent(&tools.ToolDeclaration{
		Name:            "install_skill",
		Description:     "Install a skill from a GitHub repository by cloning it into .skills/. The repository must contain SKILL.md files in a skills/ directory.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"repo_url": {
					Type:        "STRING",
					Description: "GitHub repository URL containing skills (e.g., 'https://github.com/anthropics/skills').",
				},
			},
			Required: []string{"repo_url"},
		},
	}, makeInstallSkill(skillsShDir, exec)); err != nil {
		return err
	}

	// remove_skill — write, requires consent
	if err := regConsent(&tools.ToolDeclaration{
		Name:            "remove_skill",
		Description:     "Remove an installed skills.sh skill from .skills/. Only skills from the skills.sh ecosystem can be removed; local skills in docs/skills/ cannot be removed with this tool.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"name": {
					Type:        "STRING",
					Description: "Name of the skills.sh skill to remove.",
				},
			},
			Required: []string{"name"},
		},
	}, makeRemoveSkill(skillsShDir, repo)); err != nil {
		return err
	}

	return nil
}
