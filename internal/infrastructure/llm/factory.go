// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// NewClient is the central factory for creating LLM providers.
func NewClient(cfg *config.Config, pData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
	p := cfg.GetActiveProvider()

	var authenticator auth.Authenticator
	if p.APIKey != "" {
		authenticator = &auth.APIKeyAuth{APIKey: p.APIKey}
	} else {
		authenticator = &auth.VertexAuth{}
	}

	maxBudget := cfg.ResolveThinkingBudget(p.Model, pData)

	// Currently, all requests route to Gemini.
	// Phase 2 will add branching for "openai" and "anthropic" types here.
	baseClient, err := NewGeminiClient(p.URL, p.Model, authenticator, p.ThinkingBudget, p.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch, bus)
	if err != nil {
		return nil, err
	}

	return NewResilientClient(baseClient, cfg.DisableStreaming), nil
}
