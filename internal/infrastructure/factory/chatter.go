// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	stdctx "context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_skills "github.com/gosharplite/tell-me-go/internal/infrastructure/skills"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// NewChatter builds the object graph for the orchestration layer.
func NewChatter(ctx stdctx.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
	telemetry.RegisterTraceSubscriber(deps.GetEventBus(), cfg.TracePath)

	summarizer := infra_llm.NewSummarizer(deps.GetGateway(), deps.GetEventBus(), infra_llm.WithLogger(deps.GetLogger()))

	// 1. Prepare specialized domain service dependencies.
	// Initial attempt: derive from homeDir
	homeDir := filepath.Dir(filepath.Dir(deps.GetPaths().ModeDir))
	skillsDir := filepath.Join(homeDir, "docs", "skills")

	// Workspace-aware fallback: if not in homeDir, check CWD
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		if cwd, err := os.Getwd(); err == nil {
			cwdSkills := filepath.Join(cwd, "docs", "skills")
			if _, err := os.Stat(cwdSkills); err == nil {
				skillsDir = cwdSkills
			}
		}
	}

	// Normalize and authorize the directory for AI tools
	skillsDir = filepath.Clean(skillsDir)
	if sm, ok := deps.GetSecurityManager().(interface {
		RegisterReadOnlyPath(path string)
	}); ok {
		sm.RegisterReadOnlyPath(skillsDir)
	}

	skillRepo, err := infra_skills.NewFileSkillRepository(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize skill repository: %w", err)
	}
	skillSelector := domain_skills.NewDefaultSkillSelector(skillRepo, 32000) // 32k token budget for skills

	// 2. Compose Agent Options
	opts := []agent.Option{
		agent.WithLogger(deps.GetLogger()),
		agent.WithSummarizer(summarizer),
		agent.WithSkillSelector(skillSelector),
		agent.WithInternalTools(),
		agent.WithSessionCostTracker(deps.GetTracker()),
		agent.WithPricing(cfg.Model, cfg.Mode, deps.GetPricingOverrides()),
		agent.WithSessionProvider(deps.GetSessionProvider()),
		agent.WithTurnsLogger(deps.GetTurnsLogger()),
	}

	// 3. Return the new Agent.
	reg, err := deps.GetRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve tool registry: %w", err)
	}

	return agent.NewAgent(
		deps.GetGateway(),
		deps.GetEventBus(),
		deps.GetHistoryManager(),
		cfg.ProviderName,
		reg,
		deps.GetSecurityManager(),
		opts...,
	)
}
