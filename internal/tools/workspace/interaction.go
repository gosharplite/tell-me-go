// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type interactionTool struct {
	sm domain_security.TerminalController
}

func newinteractionTool(sm domain_security.TerminalController) *interactionTool {
	return &interactionTool{sm: sm}
}

func (t *interactionTool) askUser(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Question string `json:"question"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	question := params.Question
	if question == "" {
		return tools.ToolResult{}, fmt.Errorf("question argument is required")
	}

	// Heartbeat while waiting for user input
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					sendHeartbeat(ctx, hb)
				}
			}
		}
	}()

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
