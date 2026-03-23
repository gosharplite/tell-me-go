// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/require"
)

func TestAgentOptions(t *testing.T) {
	t.Parallel()
	mockSummarizer := &mockSummarizer{}
	mockLoader := &mockLoader{}
	mockSessionLoader := &mockSessionLoader{}
	mockTracker := &mockTracker{}
	overrides := map[string]domain_pricing.ModelPricing{
		"test": {Miss: 1.0},
	}

	tests := []struct {
		name     string
		option   option
		validate func(t *testing.T, cfg *agentConfig)
	}{
		{
			name:   "WithSummarizer",
			option: withSummarizer(mockSummarizer),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockSummarizer, cfg.summarizer)
			},
		},
		{
			name:   "WithInternalTools",
			option: withInternalTools(),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.True(t, cfg.registerInternal)
			},
		},
		{
			name:   "WithPricing",
			option: withPricing("model-a", "mode-b", overrides),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, "model-a", cfg.model)
				require.Equal(t, "mode-b", cfg.mode)
				require.Equal(t, overrides, cfg.pricingOverrides)
			},
		},
		{
			name:   "WithLoader",
			option: withLoader(mockLoader),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockLoader, cfg.loader)
			},
		},
		{
			name:   "WithSessionLoader",
			option: withSessionLoader(mockSessionLoader),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockSessionLoader, cfg.sessionLoader)
			},
		},
		{
			name:   "WithSessionCostTracker",
			option: withSessionCostTracker(mockTracker),
			validate: func(t *testing.T, cfg *agentConfig) {
				require.Equal(t, mockTracker, cfg.tracker)
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
