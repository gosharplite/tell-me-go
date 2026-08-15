// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// validateChatterDeps returns an error if any required dependency is nil.
// EventBus, Paths, Gateway, HistoryManager, SkillRepository, and Summarizer are required.
// CostTracker, SecurityManager, and ConfigWatcher are intentionally excluded — they
// are optional by design and have downstream fallbacks in agent.NewAgent.
func validateChatterDeps(deps ports.ChatterComposer) error {
	if deps.GetEventBus() == nil {
		return fmt.Errorf("event bus is required")
	}
	if deps.GetPaths() == nil {
		return fmt.Errorf("paths is required")
	}
	if deps.GetGateway() == nil {
		return fmt.Errorf("gateway is required")
	}
	if deps.GetHistoryManager() == nil {
		return fmt.Errorf("history manager is required")
	}
	if deps.GetSkillRepository() == nil {
		return fmt.Errorf("skill repository is required")
	}
	if deps.GetSummarizer() == nil {
		return fmt.Errorf("summarizer is required")
	}
	return nil
}

// NewChatter builds the object graph for the orchestration layer.
func NewChatter(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
	if err := validateChatterDeps(deps); err != nil {
		return nil, err
	}

	deps.RegisterTrace(cfg.TracePath)

	summarizer := deps.GetSummarizer()
	skillRepo := deps.GetSkillRepository()
	cw := deps.GetConfigWatcher()
	if cw != nil {
		cw.SetPaths(cfg.ConfigPath, "")
	}

	skillSelector := domain_skills.NewDefaultSkillSelector(skillRepo, 32000) // 32k token budget for skills

	// Compose Agent Options
	opts := []agent.AgentOption{
		agent.WithConfigWatcher(cw),
		agent.WithLogger(deps.GetLogger()),
		agent.WithSummarizer(summarizer),
		agent.WithSkillSelector(skillSelector),
		agent.WithInternalTools(),
		agent.WithSessionCostTracker(deps.GetTracker()),
		agent.WithPricing(cfg.Model, cfg.Mode, deps.GetPricingOverrides()),
		agent.WithSessionProvider(deps.GetSessionProvider()),
		agent.WithTurnsLogger(deps.GetTurnsLogger()),
		agent.WithHistoryManager(deps.GetHistoryManager()),
		agent.WithSecurityManager(deps.GetSecurityManager()),
		agent.WithProviderName(cfg.ProviderName),
		agent.WithSkillEcosystemIntro(
			"**Skills Ecosystem**: Call `load_toolkit(names=['skillssh'])` to activate skills.sh tools: `search_skills` to discover installable skills, `list_skills` to see installed skills, `install_skill` to add new ones (requires user approval), and `remove_skill` to remove them. Installed skills are available immediately.",
		),
	}

	// Return the new Agent.
	reg, err := deps.GetRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve tool registry: %w", err)
	}

	return agent.NewAgent(
		deps.GetGateway(),
		deps.GetEventBus(),
		reg,
		opts...,
	)
}
