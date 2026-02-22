// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"os"
	"strings"
	"time"

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

	authenticator, err := createAuthenticator(&p)
	if err != nil {
		return nil, err
	}

	maxBudget := cfg.ResolveThinkingBudget(p.Model, pData)
	timeout := resolveTimeout(cfg)

	var baseClient llm.LLMClient

	switch p.Type {
	case "openai", "deepseek":
		baseClient = openai.NewClient(p.URL, p.Model, authenticator, p.Headers, cfg.Person, timeout, maxBudget)
	case "anthropic":
		baseClient = anthropic.NewClient(p.URL, p.Model, authenticator, p.Headers, maxBudget, cfg.Person, timeout)
	case "google", "gemini", "": // Default to Gemini for now
		baseClient, err = NewGeminiClient(p.URL, p.Model, authenticator, p.ThinkingBudget, p.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch, bus, timeout)
	default:
		// Fallback to Gemini if type is unknown for backward compatibility
		baseClient, err = NewGeminiClient(p.URL, p.Model, authenticator, p.ThinkingBudget, p.ThinkingLevel, maxBudget, cfg.Person, cfg.UseSearch, bus, timeout)
	}

	if err != nil {
		return nil, err
	}

	return NewResilientClient(baseClient, cfg.DisableStreaming), nil
}

func createAuthenticator(p *config.LLMProvider) (auth.Authenticator, error) {
	if p.APIKey != "" {
		// Detect if the API_KEY field is actually a path to a GCP Service Account JSON
		lowerKey := strings.ToLower(p.APIKey)
		if strings.HasSuffix(lowerKey, ".json") {
			if _, err := os.Stat(p.APIKey); err == nil {
				// Use the native Service Account provider
				return &auth.ServiceAccountAuth{KeyFilePath: p.APIKey}, nil
			}
		}

		// Fallback to provider-specific static keys
		switch p.Type {
		case "openai", "deepseek":
			return &auth.BearerAuth{Token: p.APIKey}, nil
		case "anthropic":
			return &auth.AnthropicAuth{APIKey: p.APIKey}, nil
		default:
			// Default static key for Gemini (Google AI Studio)
			return &auth.APIKeyAuth{APIKey: p.APIKey}, nil
		}
	}

	// Legacy fallback: Use VertexAuth (which depends on 'gcloud' CLI)
	if p.Type == "google" || p.Type == "gemini" || p.Type == "" {
		return &auth.VertexAuth{}, nil
	}
	return nil, fmt.Errorf("API key or Service Account JSON is required for provider: %s", p.Type)
}

func resolveTimeout(cfg *config.Config) time.Duration {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	if timeout == 0 {
		return 5 * time.Minute
	}
	return timeout
}
