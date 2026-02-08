// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
)

type InteractionTool struct {
	sm *security.SecurityManager
}

func NewInteractionTool(sm *security.SecurityManager) *InteractionTool {
	return &InteractionTool{sm: sm}
}

func (t *InteractionTool) AskUser(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Question string `json:"question"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	question := params.Question
	if question == "" {
		return tools.ToolResult{}, fmt.Errorf("question argument is required")
	}

	// Tell-me style: Question in magenta, followed by "Answer > " prompt
	fmt.Fprintf(os.Stderr, "%s[AI Question] %s%s\n", colors.ColorMagenta, question, colors.ColorReset)
	fmt.Fprintf(os.Stderr, "Answer > ")

	s, err := t.sm.ReadLine(ctx)
	if err != nil {
		if err == io.EOF {
			return tools.ToolResult{Text: "User closed input (EOF)."}, nil
		}
		return tools.ToolResult{}, fmt.Errorf("failed to read user response: %w", err)
	}

	return tools.ToolResult{Text: strings.TrimSpace(s)}, nil
}
