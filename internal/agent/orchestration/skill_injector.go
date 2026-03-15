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

	taskDescription := t.extractTaskDescription(req.History)

	selected, err := t.Selector.SelectSkills(ctx, strings.TrimSpace(taskDescription))
	if err != nil {
		return nil
	}

	if len(selected) == 0 {
		return nil
	}

	injection := t.buildInjectionBlock(selected)

	if req.History[0].Role == "system" {
		if t.isAlreadyInjected(req.History[0]) {
			return nil
		}
		t.injectToExistingSystem(req, req.History[0], injection)
		return nil
	}

	t.prependNewSystemMessage(req, injection)
	return nil
}

func (t *skillInjector) extractTaskDescription(history []*llm.Content) string {
	var taskDescription string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			for _, p := range history[i].Parts {
				if p.Text != "" {
					taskDescription += p.Text + " "
				}
			}
			if taskDescription != "" {
				break
			}
		}
	}
	return taskDescription
}

func (t *skillInjector) buildInjectionBlock(selected []skills.Skill) string {
	var sb strings.Builder
	sb.WriteString("\n\n## Relevant Go Development Skills\n")
	sb.WriteString("Use the following idiomatic patterns and best practices for this task:\n\n")

	for _, s := range selected {
		fmt.Fprintf(&sb, "### %s\n%s\n\n---\n\n", s.Name, s.Content)
	}

	return sb.String()
}

func (t *skillInjector) isAlreadyInjected(content *llm.Content) bool {
	for _, p := range content.Parts {
		if strings.Contains(p.Text, "## Relevant Go Development Skills") {
			return true
		}
	}
	return false
}

func (t *skillInjector) injectToExistingSystem(req *ports.ContextRequest, first *llm.Content, injection string) {
	first.Parts = append(first.Parts, &llm.Part{Text: injection})
	first.Pinned = true
	req.PersistHistory = true
}

func (t *skillInjector) prependNewSystemMessage(req *ports.ContextRequest, injection string) {
	newSystem := &llm.Content{
		Role:   "system",
		Pinned: true,
		Parts:  []*llm.Part{{Text: injection}},
	}
	req.History = append([]*llm.Content{newSystem}, req.History...)
	req.PersistHistory = true
}

func (t *skillInjector) Priority() int {
	// Run after history repair but before token gatekeeping to ensure they are counted
	return 10
}
