// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skillssh

import (
	"context"
	"fmt"

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
			return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
		}
		return tools.ToolResult{Text: output}, nil
	}
}
