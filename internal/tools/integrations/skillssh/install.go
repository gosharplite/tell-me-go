// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"
	"regexp"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ghRepoURL matches GitHub repository URLs: https://github.com/<owner>/<repo>
// Optional trailing slash and .git suffix are stripped during normalization.
var ghRepoURL = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`)

// makeInstallSkill returns a handler for the install_skill tool.
func makeInstallSkill(mgr SkillManager) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		var params struct {
			RepoURL string `json:"repo_url"`
		}
		if err := tools.UnmarshalArgs(args, &params); err != nil {
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}

		output, err := mgr.InstallSkill(ctx, params.RepoURL)
		if err != nil {
			return tools.ToolResult{Error: err, Text: output}, nil
		}
		return tools.ToolResult{Text: output}, nil
	}
}
