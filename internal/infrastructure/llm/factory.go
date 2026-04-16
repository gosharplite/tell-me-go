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
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/anthropic"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/gemini"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/openai"
)

// NewClient is the central factory for creating LLM providers.
func NewClient(cfg *config.Config, pData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
	p := cfg.GetActiveProvider()

	if logger == nil {
		logger = &ports.NoOpLogger{}
	}

	logger.Debug("active_provider_config",
		"name", cfg.SelectedProvider,
		"type", p.Type,
		"headers_count", len(p.Headers))

	authenticator, err := createAuthenticator(&p)
	if err != nil {
		return nil, err
	}

	maxBudget := cfg.ResolveThinkingBudget(p.Model, pData)
	timeout := resolveTimeout(cfg)

	var baseClient llm.LLMClient

	switch p.Type {
	case "openai", "deepseek":
		baseClient = openai.NewClient(p.URL, p.Model, authenticator,
			openai.WithHeaders(p.Headers),
			openai.WithPersona(cfg.Person),
			openai.WithTimeout(timeout),
			openai.WithThinkingBudget(maxBudget),
			openai.WithLogger(logger),
		)
	case "anthropic":
		baseClient = anthropic.NewClient(p.URL, p.Model, authenticator,
			anthropic.WithHeaders(p.Headers),
			anthropic.WithThinkingBudget(maxBudget),
			anthropic.WithPersona(cfg.Person),
			anthropic.WithTimeout(timeout),
			anthropic.WithLogger(logger),
		)
	case "google", "gemini", "": // Default to Gemini for now
		baseClient, err = gemini.NewClient(p.URL, p.Model, authenticator,
			gemini.WithHeaders(p.Headers),
			gemini.WithThinking(p.ThinkingBudget, p.ThinkingLevel, maxBudget),
			gemini.WithSystemInstruction(cfg.Person),
			gemini.WithSearch(cfg.UseSearch),
			gemini.WithEventBus(bus),
			gemini.WithTimeout(timeout),
			gemini.WithLogger(logger),
		)
	default:
		// Fallback to Gemini if type is unknown for backward compatibility
		baseClient, err = gemini.NewClient(p.URL, p.Model, authenticator,
			gemini.WithHeaders(p.Headers),
			gemini.WithThinking(p.ThinkingBudget, p.ThinkingLevel, maxBudget),
			gemini.WithSystemInstruction(cfg.Person),
			gemini.WithSearch(cfg.UseSearch),
			gemini.WithEventBus(bus),
			gemini.WithTimeout(timeout),
			gemini.WithLogger(logger),
		)
	}

	if err != nil {
		return nil, err
	}

	return NewResilientClient(baseClient), nil
}

func createAuthenticator(p *config.LLMProvider) (auth.Authenticator, error) {
	// Preserve the existing logic for Service Account JSON
	if p.APIKey != "" && strings.HasSuffix(strings.ToLower(p.APIKey), ".json") {
		if _, err := os.Stat(p.APIKey); err == nil {
			return &auth.ServiceAccountAuth{KeyFilePath: p.APIKey}, nil
		}
	}

	if strategy, ok := authStrategies[p.Type]; ok {
		return strategy(p)
	}

	// Fallback for any unknown provider with an explicit API key
	if p.APIKey != "" {
		return &auth.APIKeyAuth{APIKey: p.APIKey}, nil
	}

	return nil, fmt.Errorf("API key or Service Account JSON is required for provider: %s", p.Type)
}

type authStrategy func(*config.LLMProvider) (auth.Authenticator, error)

var authStrategies = map[string]authStrategy{
	"openai": func(p *config.LLMProvider) (auth.Authenticator, error) {
		if p.APIKey == "" {
			if strings.Contains(p.URL, "aiplatform.googleapis.com") {
				return &auth.VertexAuth{}, nil
			}
			return nil, fmt.Errorf("API key is required for provider: %s", p.Type)
		}
		return &auth.BearerAuth{Token: p.APIKey}, nil
	},
	"deepseek": func(p *config.LLMProvider) (auth.Authenticator, error) {
		if p.APIKey == "" {
			if strings.Contains(p.URL, "aiplatform.googleapis.com") {
				return &auth.VertexAuth{}, nil
			}
			return nil, fmt.Errorf("API key is required for provider: %s", p.Type)
		}
		return &auth.BearerAuth{Token: p.APIKey}, nil
	},
	"anthropic": func(p *config.LLMProvider) (auth.Authenticator, error) {
		if p.APIKey == "" {
			return nil, fmt.Errorf("API key is required for provider: %s", p.Type)
		}
		return &auth.AnthropicAuth{APIKey: p.APIKey}, nil
	},
	"google": resolveGoogleAuth,
	"gemini": resolveGoogleAuth,
	"":       resolveGoogleAuth,
}

func resolveGoogleAuth(p *config.LLMProvider) (auth.Authenticator, error) {
	if p.APIKey != "" {
		return &auth.APIKeyAuth{APIKey: p.APIKey}, nil
	}
	return &auth.VertexAuth{}, nil
}

func resolveTimeout(cfg *config.Config) time.Duration {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	if timeout == 0 {
		return 60 * time.Second
	}
	return timeout
}

// CreateAuthenticator exposed for toolchain/health checks.
func CreateAuthenticator(p *config.LLMProvider) (auth.Authenticator, error) {
	return createAuthenticator(p)
}
