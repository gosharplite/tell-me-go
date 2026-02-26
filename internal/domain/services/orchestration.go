// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Session encapsulates the state of a single conversation session.
type Session struct {
	ID        string
	StartTime time.Time
	History   HistoryManager
}

// NewSession creates a new Session state.
func NewSession(id string, h HistoryManager) *Session {
	return &Session{
		ID:        id,
		StartTime: time.Now(),
		History:   h,
	}
}

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	Chat(ctx context.Context, s *Session, prompt string) error
	SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error
	SetTieredThreshold(ctx context.Context, threshold int) error
	Subscribe(sub func(events.Event))
	Shutdown(ctx context.Context) error
}

// ChatterParams encapsulates all dependencies and configuration required to create a Chatter instance.
type ChatterParams struct {
	Context          context.Context
	Loader           config.ConfigLoader
	Gateway          llm.LLMGateway
	HistoryManager   HistoryManager
	Registry         tools.IToolRegistry
	SecurityManager  security.ISecurityManager
	DisableStreaming bool
	EventBus         events.EventBus
	ProviderName     string
	Model            string
	Mode             string
	LogPath          string
	PricingOverrides map[string]pricing.ModelPricing
	CostTracker      pricing.ICostTracker
}

// ChatterOption defines a functional option for ChatterParams.
type ChatterOption func(*ChatterParams)

// NewChatterParams creates a new ChatterParams with the given options.
func NewChatterParams(opts ...ChatterOption) ChatterParams {
	p := ChatterParams{
		Context:          context.Background(),
		PricingOverrides: make(map[string]pricing.ModelPricing),
	}
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

// WithContext sets the context for the Chatter instance.
func WithContext(ctx context.Context) ChatterOption {
	return func(p *ChatterParams) {
		p.Context = ctx
	}
}

// WithLoader sets the configuration loader.
func WithLoader(l config.ConfigLoader) ChatterOption {
	return func(p *ChatterParams) {
		p.Loader = l
	}
}

// WithGateway sets the LLM gateway.
func WithGateway(g llm.LLMGateway) ChatterOption {
	return func(p *ChatterParams) {
		p.Gateway = g
	}
}

// WithHistory sets the history manager.
func WithHistory(h HistoryManager) ChatterOption {
	return func(p *ChatterParams) {
		p.HistoryManager = h
	}
}

// WithToolConfig sets the tool registry.
func WithToolConfig(r tools.IToolRegistry) ChatterOption {
	return func(p *ChatterParams) {
		p.Registry = r
	}
}

// WithSecurityManager sets the security manager.
func WithSecurityManager(s security.ISecurityManager) ChatterOption {
	return func(p *ChatterParams) {
		p.SecurityManager = s
	}
}

// WithStreamingDisabled sets whether streaming is disabled.
func WithStreamingDisabled(disabled bool) ChatterOption {
	return func(p *ChatterParams) {
		p.DisableStreaming = disabled
	}
}

// WithEventBus sets the event bus.
func WithEventBus(e events.EventBus) ChatterOption {
	return func(p *ChatterParams) {
		p.EventBus = e
	}
}

// WithProvider sets the LLM provider name.
func WithProvider(provider string) ChatterOption {
	return func(p *ChatterParams) {
		p.ProviderName = provider
	}
}

// WithModel sets the LLM model name.
func WithModel(model string) ChatterOption {
	return func(p *ChatterParams) {
		p.Model = model
	}
}

// WithMode sets the operation mode.
func WithMode(mode string) ChatterOption {
	return func(p *ChatterParams) {
		p.Mode = mode
	}
}

// WithLogPath sets the path for session logs.
func WithLogPath(path string) ChatterOption {
	return func(p *ChatterParams) {
		p.LogPath = path
	}
}

// WithPricingOverrides sets the model pricing overrides.
func WithPricingOverrides(overrides map[string]pricing.ModelPricing) ChatterOption {
	return func(p *ChatterParams) {
		p.PricingOverrides = overrides
	}
}

// WithCostTracker sets the cost tracker.
func WithCostTracker(c pricing.ICostTracker) ChatterOption {
	return func(p *ChatterParams) {
		p.CostTracker = c
	}
}

// ChatterFactory defines the functional signature for creating a Chatter instance.
type ChatterFactory func(params ChatterParams) Chatter

// SessionConfig defines the configuration interface for a session.
type SessionConfig interface {
	GetPrompt() string
	GetLastN() int
	GetRawOutput() bool
	GetConfig() *config.Config
}

// SessionDependencies defines the dependencies required for a session.
type SessionDependencies interface {
	GetGateway() llm.LLMGateway
	GetHistoryManager() HistoryManager
	GetRegistry() tools.IToolRegistry
	GetEventBus() events.EventBus
	GetPaths() *persistence.Paths
	GetPricingOverrides() map[string]pricing.ModelPricing
	GetTracker() pricing.ICostTracker
	GetPricingData() pricing.PricingData
}
