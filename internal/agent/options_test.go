// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"log/slog"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/stretchr/testify/require"
)

type localMockSummarizer struct{ ports.Summarizer }
type localMockLoader struct{ domain_config.ConfigLoader }
type localMockSessionLoader struct{ domain_config.SessionLoader }
type localMockTracker struct{ domain_pricing.CostTracker }
type localMockSkillSelector struct{ skills.SkillSelector }

func TestAgentOptions(t *testing.T) {
	t.Parallel()
	mockSummarizer := &localMockSummarizer{}
	mockLoader := &localMockLoader{}
	mockSessionLoader := &localMockSessionLoader{}
	mockTracker := &localMockTracker{}
	mockLogger := slog.Default()
	mockSkillSelector := &localMockSkillSelector{}
	overrides := map[string]domain_pricing.ModelPricing{
		"test": {Miss: 1.0},
	}

	tests := []struct {
		name     string
		option   Option
		validate func(t *testing.T, cfg *agentConfig)
	}{
		{
			name:   "WithSummarizer",
			option: WithSummarizer(mockSummarizer),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockSummarizer, cfg.summarizer)
			},
		},
		{
			name:   "WithInternalTools",
			option: WithInternalTools(),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.True(t, cfg.registerInternal)
			},
		},
		{
			name:   "WithPricing",
			option: WithPricing("model-a", "mode-b", overrides),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, "model-a", cfg.model)
				require.Equal(t, "mode-b", cfg.mode)
				require.Equal(t, overrides, cfg.pricingOverrides)
			},
		},
		{
			name:   "WithLoader",
			option: WithLoader(mockLoader),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockLoader, cfg.loader)
			},
		},
		{
			name:   "WithSessionLoader",
			option: WithSessionLoader(mockSessionLoader),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockSessionLoader, cfg.sessionLoader)
			},
		},
		{
			name:   "WithSessionCostTracker",
			option: WithSessionCostTracker(mockTracker),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockTracker, cfg.tracker)
			},
		},
		{
			name:   "WithLogger",
			option: WithLogger(mockLogger),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockLogger, cfg.logger)
			},
		},
		{
			name:   "WithSkillSelector",
			option: WithSkillSelector(mockSkillSelector),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockSkillSelector, cfg.skillSelector)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &agentConfig{}
			tt.option(cfg)
			tt.validate(t, cfg)
		})
	}
}
