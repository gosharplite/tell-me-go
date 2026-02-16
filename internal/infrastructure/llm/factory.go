// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/anthropic"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/openai"
)

// NewClient is the central factory for creating LLM providers.
func NewClient(cfg *config.Config, pData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
	p := cfg.GetActiveProvider()

	var authenticator auth.Authenticator
	if p.APIKey != "" {
		switch p.Type {
		case "openai", "deepseek":
			authenticator = &auth.BearerAuth{Token: p.APIKey}
		case "anthropic":
			authenticator = &auth.AnthropicAuth{APIKey: p.APIKey}
		default:
			authenticator = &auth.APIKeyAuth{APIKey: p.APIKey}
		}
	} else {
		authenticator = &auth.VertexAuth{}
	}

	maxBudget := cfg.ResolveThinkingBudget(p.Model, pData)

	var baseClient llm.LLMClient
	var err error

	switch p.Type {
	case "openai", "deepseek":
		baseClient = openai.NewClient(p.URL, p.Model, authenticator, p.Headers, cfg.Person)
	case "anthropic":
		baseClient = anthropic.NewClient(p.URL, p.Model, authenticator, p.Headers, p.ThinkingBudget, cfg.Person)
	case "google", "gemini", "": // Default to Gemini for now
		baseClient, err = NewGeminiClient(p.URL, p.Model, authenticator, p.ThinkingBudget, p.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch, bus)
	default:
		// Fallback to Gemini if type is unknown for backward compatibility,
		// but Phase 2 will explicitly handle "openai" and "anthropic" here.
		baseClient, err = NewGeminiClient(p.URL, p.Model, authenticator, p.ThinkingBudget, p.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch, bus)
	}

	if err != nil {
		return nil, err
	}

	return NewResilientClient(baseClient, cfg.DisableStreaming), nil
}
