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
type localMockTracker struct{ domain_pricing.CostTracker }
type localMockSkillSelector struct{ skills.SkillSelector }

func TestAgentOptions(t *testing.T) {
	t.Parallel()
	mockSummarizer := &localMockSummarizer{}
	mockTracker := &localMockTracker{}
	mockLogger := slog.Default()
	mockSkillSelector := &localMockSkillSelector{}
	mockConfigWatcher := domain_config.NewNoOpConfigWatcher(100, 10, 20)
	overrides := map[string]domain_pricing.ModelPricing{
		"test": {Miss: 1.0},
	}

	tests := []struct {
		name     string
		option   AgentOption
		validate func(t *testing.T, a *agent)
	}{
		{
			name:   "WithSummarizer",
			option: WithSummarizer(mockSummarizer),
			validate: func(t *testing.T, a *agent) {
				require.Equal(t, mockSummarizer, a.summarizer)
			},
		},
		{
			name:   "WithInternalTools",
			option: WithInternalTools(),
			validate: func(t *testing.T, a *agent) {
				require.True(t, a.registerInternal)
			},
		},
		{
			name:   "WithPricing",
			option: WithPricing("model-a", "mode-b", overrides),
			validate: func(t *testing.T, a *agent) {
				require.Equal(t, "model-a", a.model)
				require.Equal(t, "mode-b", a.mode)
				require.Equal(t, overrides, a.pricingOverrides)
			},
		},
		{
			name:   "WithConfigWatcher",
			option: WithConfigWatcher(mockConfigWatcher),
			validate: func(t *testing.T, a *agent) {
				require.Equal(t, mockConfigWatcher, a.configWatcher)
			},
		},
		{
			name:   "WithSessionCostTracker",
			option: WithSessionCostTracker(mockTracker),
			validate: func(t *testing.T, a *agent) {
				require.Equal(t, mockTracker, a.tracker)
			},
		},
		{
			name:   "WithLogger",
			option: WithLogger(mockLogger),
			validate: func(t *testing.T, a *agent) {
				require.Equal(t, mockLogger, a.logger)
			},
		},
		{
			name:   "WithSkillSelector",
			option: WithSkillSelector(mockSkillSelector),
			validate: func(t *testing.T, a *agent) {
				require.Equal(t, mockSkillSelector, a.skillSelector)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &agent{}
			tt.option(a)
			tt.validate(t, a)
		})
	}
}
