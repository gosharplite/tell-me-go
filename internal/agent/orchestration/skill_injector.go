// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// skillInjector dynamically selects and injects Go development skills into the context.
type skillInjector struct {
	Selector skills.SkillSelector
}

func (t *skillInjector) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if t.Selector == nil || len(req.History) == 0 {
		return nil
	}

	// 1. Identify the core task/prompt for selection.
	// We use the last user message as the primary hint for skill selection.
	var taskDescription string
	for i := len(req.History) - 1; i >= 0; i-- {
		if req.History[i].Role == "user" {
			for _, p := range req.History[i].Parts {
				if p.Text != "" {
					taskDescription += p.Text + " "
				}
			}
			if taskDescription != "" {
				break
			}
		}
	}

	// 2. Select relevant skills based on the description.
	selected, err := t.Selector.SelectSkills(ctx, strings.TrimSpace(taskDescription))
	if err != nil {
		// Non-terminal: log and continue without skills
		return nil
	}

	if len(selected) == 0 {
		return nil
	}

	// 3. Construct the injection block.
	var sb strings.Builder
	sb.WriteString("\n\n## Relevant Go Development Skills\n")
	sb.WriteString("Use the following idiomatic patterns and best practices for this task:\n\n")

	for _, s := range selected {
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n---\n\n", s.Name, s.Content))
	}

	injection := sb.String()

	// 4. Inject into the SYSTEM prompt if it exists, otherwise the first message.
	// In this system, we assume the first message is either SYSTEM or the starting USER message.
	first := req.History[0]

	// Check if already injected to maintain idempotency within a session view
	for _, p := range first.Parts {
		if strings.Contains(p.Text, "## Relevant Go Development Skills") {
			return nil
		}
	}
	for _, p := range first.TransientParts {
		if strings.Contains(p.Text, "## Relevant Go Development Skills") {
			return nil
		}
	}

	// Append to TransientParts so it doesn't persist to disk history (as per ADR-0005 decision)
	first.TransientParts = append(first.TransientParts, &llm.Part{
		Text: injection,
	})

	return nil
}

func (t *skillInjector) Priority() int {
	// Run after history repair but before token gatekeeping to ensure they are counted
	return 10
}
