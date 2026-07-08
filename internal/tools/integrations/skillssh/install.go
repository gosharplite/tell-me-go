// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ghRepoURL matches GitHub repository URLs: https://github.com/<owner>/<repo>
// Optional trailing slash and .git suffix are stripped during normalization.
var ghRepoURL = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`)

// makeInstallSkill returns a handler for the install_skill tool.
func makeInstallSkill(skillsShDir string, exec ExecRunner) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var params struct {
			RepoURL string `json:"repo_url"`
		}
		if err := tools.UnmarshalArgs(args, &params); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}

		repoURL := strings.TrimSpace(params.RepoURL)
		if repoURL == "" {
			return tools.ToolResult{Text: "Error: repo_url is required."}, nil
		}

		// Validate URL format
		matches := ghRepoURL.FindStringSubmatch(repoURL)
		if matches == nil {
			return tools.ToolResult{
				Text: fmt.Sprintf("Error: invalid GitHub repository URL: %s\n\nURL must be in the format: https://github.com/<owner>/<repo>", repoURL),
			}, nil
		}

		owner := matches[1]
		repo := matches[2]

		// Derive target directory: $TELL_ME_HOME/.skills/<owner>-<repo>/
		targetDir := filepath.Join(skillsShDir, fmt.Sprintf("%s-%s", owner, repo))

		// Check if already installed
		if _, err := os.Stat(targetDir); err == nil {
			return tools.ToolResult{
				Text: fmt.Sprintf("Skill repository %s/%s is already installed at %s.\n\nUse `list_skills` to see installed skills or `remove_skill` to remove it first.", owner, repo, targetDir),
			}, nil
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(skillsShDir, 0755); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error creating .skills/ directory: %v", err)}, nil
		}

		// Run git clone
		if exec == nil {
			return tools.ToolResult{Text: "Error: command execution is not available."}, nil
		}

		output, err := exec(ctx, "git", "clone", repoURL, targetDir)
		if err != nil {
			return tools.ToolResult{
				Error: err,
				Text:  fmt.Sprintf("Error cloning repository:\n%s\n\nError: %v", string(output), err),
			}, nil
		}

		return tools.ToolResult{
			Text: fmt.Sprintf("Successfully installed skills from %s/%s to %s\n\n%s\n\nInstalled skills will be available on the next message. Use `list_skills` to see them.", owner, repo, targetDir, string(output)),
		}, nil
	}
}
