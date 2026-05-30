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

// softMaxTokensCeiling is a heuristic threshold above any current
// published model ceiling as of 2026-04. Above this we emit a one-time
// warning so operators notice obviously-wrong values without blocking;
// the API will reject if the value actually exceeds the model's hard
// ceiling. Raise this constant as model ceilings rise.
//
// Pinned by TestFactory_MaxTokensAboveSoftCeiling_EmitsWarning.
const softMaxTokensCeiling = 200_000

// buildBaseClient constructs a provider-specific LLM client based on the
// provider type. Unknown or empty types fall back to Gemini. The caller
// must provide a pre-created authenticator.
func buildBaseClient(p config.LLMProvider, authenticator auth.Authenticator, persona string, useSearch bool, timeout time.Duration, maxBudget int, bus events.EventBus, logger ports.Logger) (llm.LLMClient, error) {
	var baseClient llm.LLMClient
	var err error

	switch p.Type {
	case "openai", "deepseek":
		baseClient = openai.NewClient(p.URL, p.Model, authenticator,
			openai.WithHeaders(p.Headers),
			openai.WithPersona(persona),
			openai.WithTimeout(timeout),
			openai.WithThinkingBudget(maxBudget),
			openai.WithMaxTokens(p.MaxTokens),
			openai.WithLogger(logger),
		)
	case "anthropic":
		baseClient = anthropic.NewClient(p.URL, p.Model, authenticator,
			anthropic.WithHeaders(p.Headers),
			anthropic.WithThinkingBudget(maxBudget),
			anthropic.WithMaxTokens(p.MaxTokens),
			anthropic.WithPersona(persona),
			anthropic.WithTimeout(timeout),
			anthropic.WithLogger(logger),
		)
	case "google", "gemini", "":
		baseClient, err = gemini.NewClient(p.URL, p.Model, authenticator,
			gemini.WithHeaders(p.Headers),
			gemini.WithThinking(p.ThinkingBudget, p.ThinkingLevel, maxBudget),
			gemini.WithMaxOutputTokens(p.MaxTokens),
			gemini.WithSystemInstruction(persona),
			gemini.WithSearch(useSearch),
			gemini.WithEventBus(bus),
			gemini.WithTimeout(timeout),
			gemini.WithLogger(logger),
		)
	default:
		baseClient, err = gemini.NewClient(p.URL, p.Model, authenticator,
			gemini.WithHeaders(p.Headers),
			gemini.WithThinking(p.ThinkingBudget, p.ThinkingLevel, maxBudget),
			gemini.WithMaxOutputTokens(p.MaxTokens),
			gemini.WithSystemInstruction(persona),
			gemini.WithSearch(useSearch),
			gemini.WithEventBus(bus),
			gemini.WithTimeout(timeout),
			gemini.WithLogger(logger),
		)
	}

	return baseClient, err
}

// newClient is the central factory for creating LLM provider clients.
//
// It inspects cfg.GetActiveProvider() to determine the provider type
// ("openai", "deepseek", "anthropic", "google", "gemini"), creates the
// appropriate authenticator, resolves timeout and thinking budget, and
// instantiates a provider-specific client. The resulting client is
// wrapped in a resilientClient for automatic retry on transient failures.
//
// Parameters:
//   - cfg: must have at least one configured provider with a non-empty
//     Type. The active provider is determined by cfg.SelectedProvider.
//   - pData: pricing data used to resolve per-model token limits and
//     thinking budget caps.
//   - bus: event bus for publishing usage metrics and diagnostics.
//     May be nil, in which case metrics are not published.
//   - logger: if nil, a NoOpLogger is used. The logger receives
//     provider configuration diagnostics and soft warnings.
//
// Returns an llm.ExtendedClient ready for concurrent use, or an error
// if no provider is configured or authentication setup fails. Unknown
// provider types fall back to Gemini for backward compatibility.
func newClient(cfg *config.Config, pData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
	p := cfg.GetActiveProvider()

	if logger == nil {
		logger = &ports.NoOpLogger{}
	}

	logger.Debug("active_provider_config",
		"name", cfg.SelectedProvider,
		"type", p.Type,
		"headers_count", len(p.Headers))

	if p.MaxTokens > softMaxTokensCeiling {
		logger.Warn("provider_max_tokens_unusually_high",
			"provider", cfg.SelectedProvider,
			"value", p.MaxTokens,
			"note", "exceeds typical model ceilings; the API will reject if too large")
	}

	authenticator, err := createAuthenticator(&p)
	if err != nil {
		return nil, err
	}

	maxBudget := cfg.ResolveThinkingBudget(p.Model, pData)
	timeout := resolveTimeout(cfg)

	baseClient, err := buildBaseClient(p, authenticator, cfg.Person, cfg.UseSearch, timeout, maxBudget, bus, logger)
	if err != nil {
		return nil, err
	}

	return NewResilientClient(baseClient), nil
}

// newFailoverChain creates a FailoverGateway from the configured failover
// provider order. It constructs a resilientClient for each provider in the
// chain and wraps them in a FailoverGateway for transparent failover.
//
// When cfg.FailoverOrder is empty, this returns nil, nil — callers should
// fall back to newClient for single-provider operation.
//
// Each client in the chain is independently constructed via the same
// per-provider logic as newClient (authenticator, timeout, thinking budget,
// headers), but wrapped in a resilientClient instead of returned directly.
func newFailoverChain(cfg *config.Config, pData pricing.PricingData, bus events.EventBus, logger ports.Logger) (*FailoverGateway, error) {
	providers := cfg.GetFailoverProviders()
	if len(providers) == 0 {
		return nil, nil
	}

	if logger == nil {
		logger = &ports.NoOpLogger{}
	}

	timeout := resolveTimeout(cfg)
	clients := make([]NamedClient, 0, len(providers))

	for _, provider := range providers {
		p := provider // capture range variable

		authenticator, err := createAuthenticator(&p)
		if err != nil {
			return nil, fmt.Errorf("failover chain: provider %q: %w", p.Type, err)
		}

		maxBudget := cfg.ResolveThinkingBudget(p.Model, pData)

		baseClient, err := buildBaseClient(p, authenticator, cfg.Person, cfg.UseSearch, timeout, maxBudget, bus, logger)
		if err != nil {
			return nil, fmt.Errorf("failover chain: provider %q: %w", p.Type, err)
		}

		clients = append(clients, NamedClient{
			Name:   p.Type,
			Client: NewResilientClient(baseClient),
		})
	}

	return NewFailoverGateway(clients), nil
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
				return auth.NewVertexAuth(), nil
			}
			return nil, fmt.Errorf("API key is required for provider: %s", p.Type)
		}
		return &auth.BearerAuth{Token: p.APIKey}, nil
	},
	"deepseek": func(p *config.LLMProvider) (auth.Authenticator, error) {
		if p.APIKey == "" {
			if strings.Contains(p.URL, "aiplatform.googleapis.com") {
				return auth.NewVertexAuth(), nil
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
	return auth.NewVertexAuth(), nil
}

func resolveTimeout(cfg *config.Config) time.Duration {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	if timeout == 0 {
		return 60 * time.Second
	}
	return timeout
}

// CreateAuthenticator creates an authenticator for the given provider
// configuration. It is the public entry point for authentication setup,
// used by the toolchain and health check subsystems.
//
// The authenticator type is determined by the provider type and
// available credentials:
//   - APIKey: BearerAuth (OpenAI/DeepSeek), AnthropicAuth, or
//     APIKeyAuth (Google/Gemini with explicit key)
//   - Service Account JSON file: ServiceAccountAuth
//   - Google/Gemini without API key: VertexAuth (Application Default
//     Credentials)
//
// Returns an error if no credentials are available and the provider
// does not support credential-less authentication.
func CreateAuthenticator(p *config.LLMProvider) (auth.Authenticator, error) {
	return createAuthenticator(p)
}

// DefaultClientFactory implements ports.ClientFactory by delegating to
// the canonical package-level constructor functions (newClient and
// newFailoverChain). It is the production implementation wired by
// DefaultBootstrapperConfig.
type DefaultClientFactory struct{}

// NewClient delegates to the package-level newClient constructor.
func (f *DefaultClientFactory) NewClient(cfg *config.Config, pData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
	return newClient(cfg, pData, bus, logger)
}

// NewFailoverChain delegates to the package-level newFailoverChain constructor.
// The nil-return convention is preserved: when no failover providers are
// configured, the underlying newFailoverChain returns (nil, nil), and this
// method propagates that through the llm.ExtendedClient interface.
func (f *DefaultClientFactory) NewFailoverChain(cfg *config.Config, pData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
	gw, err := newFailoverChain(cfg, pData, bus, logger)
	if err != nil {
		return nil, err
	}
	return gw, nil
}

// Compile-time verification that DefaultClientFactory satisfies ports.ClientFactory.
var _ ports.ClientFactory = (*DefaultClientFactory)(nil)
