// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	stdctx "context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
	infra_config "github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_skills "github.com/gosharplite/tell-me-go/internal/infrastructure/skills"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// resolveSkillsDir finds the skills directory, falling back from homeDir to CWD.
func resolveSkillsDir(paths *persistence.Paths) string {
	homeDir := filepath.Dir(filepath.Dir(paths.ModeDir))
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

	return filepath.Clean(skillsDir)
}

// resolveSkillsShDir finds the .skills/ directory (skills.sh format) under
// $TELL_ME_HOME. Unlike resolveSkillsDir, there is no CWD fallback — skills.sh
// repositories are only expected under the TELL_ME_HOME directory.
func resolveSkillsShDir(paths *persistence.Paths) string {
	homeDir := filepath.Dir(filepath.Dir(paths.ModeDir))
	return filepath.Join(homeDir, ".skills")
}

// registerReadOnlySkillsPath registers the skills directory with the security
// manager if it supports the RegisterReadOnlyPath method.
func registerReadOnlySkillsPath(deps ports.ChatterComposer, dir string) {
	if sm, ok := deps.GetSecurityManager().(interface {
		RegisterReadOnlyPath(path string)
	}); ok {
		sm.RegisterReadOnlyPath(dir)
	}
}

// validateChatterDeps returns an error if any required dependency is nil.
// CostTracker and SecurityManager are intentionally excluded — both may
// be nil by design and have downstream guards in agent.NewAgent.
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
	return nil
}

// NewChatter builds the object graph for the orchestration layer.
func NewChatter(ctx stdctx.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
	if err := validateChatterDeps(deps); err != nil {
		return nil, err
	}

	telemetry.RegisterTraceSubscriber(deps.GetEventBus(), cfg.TracePath)

	summarizer := infra_llm.NewSummarizer(deps.GetGateway(), deps.GetEventBus(), infra_llm.WithLogger(deps.GetLogger()))

	skillsDir := resolveSkillsDir(deps.GetPaths())

	registerReadOnlySkillsPath(deps, skillsDir)

	// Use the shared SkillRepository if provided by the composition root.
	// Otherwise fall back to building one locally (backward compat for tests).
	skillRepo := deps.GetSkillRepository()
	if skillRepo == nil {
		fileRepo, err := infra_skills.NewFileSkillRepository(skillsDir)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize skill repository: %w", err)
		}

		// .skills/ — skills.sh format (optional; degrades gracefully)
		skillsShDir := resolveSkillsShDir(deps.GetPaths())
		registerReadOnlySkillsPath(deps, skillsShDir)

		skillsShRepo, err := infra_skills.NewSkillsShRepository(skillsShDir)
		if err != nil {
			slog.Warn("failed to initialize .skills/ repository, continuing without it",
				"error", err)
			skillsShRepo = nil
		}

		// Composite merges both sources. docs/skills/ goes first so
		// Dobby-curated skills take priority in tie-breaking.
		if skillsShRepo != nil {
			skillRepo = &infra_skills.CompositeRepository{
				Repos: []domain_skills.SkillRepository{fileRepo, skillsShRepo},
			}
		} else {
			skillRepo = fileRepo
		}
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
	cw.SetPaths(cfg.ConfigPath, "")

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
		agent.WithSkillEcosystemIntro(
			"**Skills Ecosystem**: Call `load_toolkit(names=['skillssh'])` to activate skills.sh tools: `search_skills` to discover installable skills, `list_skills` to see installed skills, `install_skill` to add new ones (requires user approval), and `remove_skill` to remove them. Installed skills become available on the next message.",
		),
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
