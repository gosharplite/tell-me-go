// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

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

// chatterOption defines a functional option for ChatterParams.
type chatterOption func(*ChatterParams)

// NewChatterParams creates a new ChatterParams with the given options.
func NewChatterParams(opts ...chatterOption) ChatterParams {
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
func WithContext(ctx context.Context) chatterOption {
	return func(p *ChatterParams) {
		p.Context = ctx
	}
}

// WithLoader sets the configuration loader.
func WithLoader(l config.ConfigLoader) chatterOption {
	return func(p *ChatterParams) {
		p.Loader = l
	}
}

// WithGateway sets the LLM gateway.
func WithGateway(g llm.LLMGateway) chatterOption {
	return func(p *ChatterParams) {
		p.Gateway = g
	}
}

// WithHistory sets the history manager.
func WithHistory(h HistoryManager) chatterOption {
	return func(p *ChatterParams) {
		p.HistoryManager = h
	}
}

// WithToolConfig sets the tool registry.
func WithToolConfig(r tools.IToolRegistry) chatterOption {
	return func(p *ChatterParams) {
		p.Registry = r
	}
}

// WithSecurityManager sets the security manager.
func WithSecurityManager(s security.ISecurityManager) chatterOption {
	return func(p *ChatterParams) {
		p.SecurityManager = s
	}
}

// WithStreamingDisabled sets whether streaming is disabled.
func WithStreamingDisabled(disabled bool) chatterOption {
	return func(p *ChatterParams) {
		p.DisableStreaming = disabled
	}
}

// WithEventBus sets the event bus.
func WithEventBus(e events.EventBus) chatterOption {
	return func(p *ChatterParams) {
		p.EventBus = e
	}
}

// WithProvider sets the LLM provider name.
func WithProvider(provider string) chatterOption {
	return func(p *ChatterParams) {
		p.ProviderName = provider
	}
}

// WithModel sets the LLM model name.
func WithModel(model string) chatterOption {
	return func(p *ChatterParams) {
		p.Model = model
	}
}

// WithMode sets the operation mode.
func WithMode(mode string) chatterOption {
	return func(p *ChatterParams) {
		p.Mode = mode
	}
}

// WithLogPath sets the path for session logs.
func WithLogPath(path string) chatterOption {
	return func(p *ChatterParams) {
		p.LogPath = path
	}
}

// WithPricingOverrides sets the model pricing overrides.
func WithPricingOverrides(overrides map[string]pricing.ModelPricing) chatterOption {
	return func(p *ChatterParams) {
		p.PricingOverrides = overrides
	}
}

// WithCostTracker sets the cost tracker.
func WithCostTracker(c pricing.ICostTracker) chatterOption {
	return func(p *ChatterParams) {
		p.CostTracker = c
	}
}

// ChatterFactory defines the functional signature for creating a Chatter instance.
type ChatterFactory func(params ChatterParams) (Chatter, error)

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

// ContextMetadata contains diagnostics and auxiliary data from the pipeline.
type ContextMetadata struct {
	OriginalTokenCount     int
	FinalTokenCount        int
	FinalTurnCount         int
	PrunedTurns            int
	SummarizedTurns        int
	SummarizationAttempted bool
	MaintenanceBlocked     bool
	Warnings               []string
	TotalTurnsKept         int
	KeptByPolicy           map[string]int
	History                []*llm.Content
}

// ContextRequest represents the input and state of a context preparation pipeline.
type ContextRequest struct {
	Turn           int
	History        []*llm.Content
	Metadata       ContextMetadata
	PersistHistory bool
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error)
	Name() string
}

// ContextTransformer modifies the context before it's sent to the LLM.
type ContextTransformer interface {
	Transform(ctx context.Context, req *ContextRequest) error
	Priority() int
}

// ResultStrategy defines how tool outputs are transformed back into LLM messages.
type ResultStrategy interface {
	Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part
}

// Clone creates a deep copy of the ContextMetadata.
func (m *ContextMetadata) Clone() *ContextMetadata {
	cloned := *m
	if m.Warnings != nil {
		cloned.Warnings = make([]string, len(m.Warnings))
		copy(cloned.Warnings, m.Warnings)
	}
	if m.KeptByPolicy != nil {
		cloned.KeptByPolicy = make(map[string]int)
		for k, v := range m.KeptByPolicy {
			cloned.KeptByPolicy[k] = v
		}
	}
	if m.History != nil {
		cloned.History = make([]*llm.Content, len(m.History))
		for i, c := range m.History {
			cloned.History[i] = llm.CloneContent(c)
		}
	}
	return &cloned
}
