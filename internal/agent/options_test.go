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
		option   AgentOption
		validate func(t *testing.T, a *Agent)
	}{
		{
			name:   "WithSummarizer",
			option: WithSummarizer(mockSummarizer),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, mockSummarizer, a.summarizer)
			},
		},
		{
			name:   "WithInternalTools",
			option: WithInternalTools(),
			validate: func(t *testing.T, a *Agent) {
				require.True(t, a.registerInternal)
			},
		},
		{
			name:   "WithPricing",
			option: WithPricing("model-a", "mode-b", overrides),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, "model-a", a.model)
				require.Equal(t, "mode-b", a.mode)
				require.Equal(t, overrides, a.pricingOverrides)
			},
		},
		{
			name:   "WithLoader",
			option: WithLoader(mockLoader),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, mockLoader, a.loader)
			},
		},
		{
			name:   "WithSessionLoader",
			option: WithSessionLoader(mockSessionLoader),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, mockSessionLoader, a.sessionLoader)
			},
		},
		{
			name:   "WithSessionCostTracker",
			option: WithSessionCostTracker(mockTracker),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, mockTracker, a.tracker)
			},
		},
		{
			name:   "WithLogger",
			option: WithLogger(mockLogger),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, mockLogger, a.logger)
			},
		},
		{
			name:   "WithSkillSelector",
			option: WithSkillSelector(mockSkillSelector),
			validate: func(t *testing.T, a *Agent) {
				require.Equal(t, mockSkillSelector, a.skillSelector)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Agent{}
			tt.option(a)
			tt.validate(t, a)
		})
	}
}
