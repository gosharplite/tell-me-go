// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// HistoryManagerProvider defines the interface for providing history-related services.
// NOTE: This interface is for infrastructure use and should be resolved by the CLI/Wiring layer.
type HistoryManagerProvider interface {
	// GetHistoryManager loads the history manager for a given configuration.
	GetHistoryManager(ctx context.Context, cfg *config.Config) (HistoryManager, error)

	// GetUnifiedHistoryProvider assembles the read-model for the history browser.
	GetUnifiedHistoryProvider(ctx context.Context, cfg *config.Config, hManager HistoryManager) (UnifiedHistoryProvider, error)
}

// SuggestionProvider defines the interface for providing suggestion services.
// NOTE: This interface is for infrastructure use and should be resolved by the CLI/Wiring layer.
type SuggestionProvider interface {
	// GetSuggestionService initializes and returns the suggestion service.
	GetSuggestionService(ctx context.Context, recentHistory []string) (SuggestionService, error)
}

// ClientFactory abstracts LLM client creation for the DI container.
// The two methods share an identical parameter signature — each receives
// the full configuration, pricing data, event bus, and logger needed
// to construct a provider client or failover chain.
//
// Concrete implementations live in internal/infrastructure/llm.
type ClientFactory interface {
	NewClient(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error)
	NewFailoverChain(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error)
}

// ClientFactoryFunc adapts a single-client factory function to the ClientFactory
// interface. NewFailoverChain returns nil, nil — callers fall back to NewClient.
//
// This adapter preserves backward compatibility with all existing tests that
// assign anonymous functions to BootstrapperConfig.ClientFactory.
type ClientFactoryFunc func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error)

func (f ClientFactoryFunc) NewClient(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
	return f(cfg, pricingData, bus, logger)
}

func (f ClientFactoryFunc) NewFailoverChain(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger Logger) (llm.ExtendedClient, error) {
	return nil, nil
}
