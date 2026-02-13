// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"io"
	"strings"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

type interactionTool struct {
	sm domain_security.ISecurityManager
}

func newinteractionTool(sm domain_security.ISecurityManager) *interactionTool {
	return &interactionTool{sm: sm}
}

func (t *interactionTool) AskUser(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	// Tell-me style: Question, followed by "Answer > " prompt
	t.sm.Warn(fmt.Sprintf("[AI Question] %s", question))
	t.sm.Prompt("Answer > ")

	s, err := t.sm.ReadLine(ctx)
	if err != nil {
		if err == io.EOF {
			return tools.ToolResult{Text: "User closed input (EOF)."}, nil
		}
		return tools.ToolResult{}, fmt.Errorf("failed to read user response: %w", err)
	}

	return tools.ToolResult{Text: strings.TrimSpace(s)}, nil
}
