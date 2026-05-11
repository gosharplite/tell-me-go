// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	stdctx "context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
	infra_config "github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_skills "github.com/gosharplite/tell-me-go/internal/infrastructure/skills"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// NewChatter builds the object graph for the orchestration layer.
func NewChatter(ctx stdctx.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
	// Required dependency validation.
	// Note: CostTracker may be nil by design — the engine provides a no-op fallback.
	// Note: SecurityManager nil-check is intentionally deferred to downstream
	// guards in agent.NewAgent; the type assertion below safely handles nil
	// (ok == false) and the agent's initComponents provides a no-op fallback.
	if deps.GetEventBus() == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if deps.GetPaths() == nil {
		return nil, fmt.Errorf("paths is required")
	}
	if deps.GetGateway() == nil {
		return nil, fmt.Errorf("gateway is required")
	}
	if deps.GetHistoryManager() == nil {
		return nil, fmt.Errorf("history manager is required")
	}

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

	// Build config watcher: production always uses file-based watcher.
	// The agent's nil-guard in initComponents() provides a no-op fallback
	// for bare construction (e.g. tests) where no option is passed.
	cw := infra_config.NewFileConfigWatcher(
		&infra_config.YAMLConfigLoader{Finder: infra_config.NewDefaultConfigFinder()},
		&infra_config.JSONSessionLoader{},
		domain_config.DefaultMaxHistoryTokens,
		domain_config.DefaultMaxToolTurns,
		domain_config.DefaultMaxHistoryTurns,
		deps.GetLogger(),
	)

	// 2. Compose Agent Options
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
	}

	// 3. Return the new Agent.
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
