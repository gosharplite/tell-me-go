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

	// 4. Inject into the SYSTEM prompt if it exists, otherwise prepend a new system message.
	// This ensures prefix caching compatibility (stable system block) and persistence.
	first := req.History[0]

	// Check for idempotency: iterate through the parts of the system message to ensure
	// "## Relevant Go Development Skills" is not already present.
	if first.Role == "system" {
		for _, p := range first.Parts {
			if strings.Contains(p.Text, "## Relevant Go Development Skills") {
				return nil
			}
		}

		// Inject into existing system message
		first.Parts = append(first.Parts, &llm.Part{Text: injection})
		first.Pinned = true
		req.PersistHistory = true
		return nil
	}

	// First message is not a system message, prepend one.
	newSystem := &llm.Content{
		Role:   "system",
		Pinned: true,
		Parts:  []*llm.Part{{Text: injection}},
	}
	req.History = append([]*llm.Content{newSystem}, req.History...)
	req.PersistHistory = true

	return nil
}

func (t *skillInjector) Priority() int {
	// Run after history repair but before token gatekeeping to ensure they are counted
	return 10
}
