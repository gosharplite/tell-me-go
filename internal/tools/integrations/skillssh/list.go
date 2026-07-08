// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// makeListSkills returns a handler for the list_skills tool.
func makeListSkills(mgr SkillManager) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		output, err := mgr.ListSkills(ctx)
		if err != nil {
			return tools.ToolResult{Error: err, Text: output}, nil
		}
		return tools.ToolResult{Text: output}, nil
	}
}
