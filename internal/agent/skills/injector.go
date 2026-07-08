// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package skills

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
	Selector       skills.SkillSelector
	Logger         ports.Logger
	ecosystemIntro string
}

// SkillInjectorOption configures a skillInjector.
type SkillInjectorOption func(*skillInjector)

// WithSkillEcosystemIntro sets an optional ecosystem introduction that is
// appended to the skill injection block. This allows the infrastructure
// layer to advertise available toolkits (e.g., skills.sh) without the
// agent package knowing about specific implementations.
func WithSkillEcosystemIntro(intro string) SkillInjectorOption {
	return func(si *skillInjector) { si.ecosystemIntro = intro }
}

// NewSkillInjector creates a ports.ContextTransformer that injects skills
// into the context. It exists as a constructor so the session/ package can
// inject skill selection into the session/context pipeline without the
// context/ sub-package importing domain/skills.
func NewSkillInjector(selector skills.SkillSelector, logger ports.Logger, opts ...SkillInjectorOption) ports.ContextTransformer {
	si := &skillInjector{Selector: selector, Logger: logger}
	for _, opt := range opts {
		opt(si)
	}
	return si
}

func (t *skillInjector) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if t.Selector == nil || len(req.History) == 0 {
		return nil
	}

	taskDescription := t.extractTaskDescription(req.History)

	selected, err := t.Selector.SelectSkills(ctx, strings.TrimSpace(taskDescription))
	if err != nil {
		if t.Logger != nil {
			t.Logger.Warn("skill selection failed; proceeding without injected skills", "error", err)
		}
		// Don't return — still inject ecosystem intro if configured
	}

	// Always inject ecosystem intro if configured, even when no skills matched.
	// This ensures the LLM knows about available toolkits on every turn.
	// The ecosystem intro alone does NOT trigger history persistence — only
	// actual skill injection does.
	if t.ecosystemIntro != "" && len(selected) == 0 {
		injection := "\n\n" + t.ecosystemIntro + "\n"
		t.injectIfNeeded(req, injection, false)
		return nil
	}

	if len(selected) == 0 {
		return nil
	}

	injection := t.buildInjectionBlock(selected)
	t.injectIfNeeded(req, injection, true)
	return nil
}

// injectIfNeeded adds the injection string to the request history,
// either by appending to an existing system message or by prepending
// a new one. It also guards against double-injection. The persist
// flag controls whether req.PersistHistory is set — it should be
// true only when skill content is injected, not for ecosystem intro.
func (t *skillInjector) injectIfNeeded(req *ports.ContextRequest, injection string, persist bool) {
	if req.History[0].Role == "system" {
		if t.isAlreadyInjected(req.History[0]) {
			return
		}
		t.injectToExistingSystem(req, req.History[0], injection, persist)
		return
	}
	t.prependNewSystemMessage(req, injection, persist)
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

	sb.WriteString("\n")

	if t.ecosystemIntro != "" {
		sb.WriteString(t.ecosystemIntro)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (t *skillInjector) isAlreadyInjected(content *llm.Content) bool {
	for _, p := range content.Parts {
		if strings.Contains(p.Text, "## Relevant Go Development Skills") {
			return true
		}
		if t.ecosystemIntro != "" && strings.Contains(p.Text, t.ecosystemIntro) {
			return true
		}
	}
	return false
}

func (t *skillInjector) injectToExistingSystem(req *ports.ContextRequest, first *llm.Content, injection string, persist bool) {
	first.Parts = append(first.Parts, &llm.Part{Text: injection})
	first.Pinned = true
	if persist {
		req.PersistHistory = true
	}
}

func (t *skillInjector) prependNewSystemMessage(req *ports.ContextRequest, injection string, persist bool) {
	newSystem := &llm.Content{
		Role:   "system",
		Pinned: true,
		Parts:  []*llm.Part{{Text: injection}},
	}
	req.History = append([]*llm.Content{newSystem}, req.History...)
	if persist {
		req.PersistHistory = true
	}
}

func (t *skillInjector) Priority() int {
	// Run after history repair but before token gatekeeping to ensure they are counted
	return 10
}
