// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"log/slog"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const (
	// DefaultShutdownTimeout defines the standard grace period for application cleanup and flushing
	// asynchronous buffers (e.g., suggestion persistence, telemetry events).
	DefaultShutdownTimeout = 7 * time.Second
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

// ChatExecutor defines the interface for executing chat turns and lifecycle management.
type ChatExecutor interface {
	Chat(ctx context.Context, s *Session, prompt string) error
	Shutdown(ctx context.Context) error
}

// ChatConfigurator defines the interface for configuring chat parameters.
type ChatConfigurator interface {
	SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error
	SetTieredThreshold(ctx context.Context, threshold int) error
}

// ChatEventSource defines the interface for subscribing to chat events.
type ChatEventSource interface {
	Subscribe(sub func(context.Context, events.Event))
}

// Chatter defines the full interface for the AI agent session management.
type Chatter interface {
	ChatExecutor
	ChatConfigurator
	ChatEventSource
}

// ChatterConfig encapsulates the primitive configuration for a Chatter instance.
type ChatterConfig struct {
	ProviderName string
	Model        string
	Mode         string
	LogPath      string
	TracePath    string
}

// ChatterFactory defines the functional signature for creating a Chatter instance.
type ChatterFactory func(ctx context.Context, deps SessionDependencies, cfg ChatterConfig) (Chatter, error)

// SessionConfig defines the configuration interface for a session.
type SessionConfig interface {
	GetPrompt() string
	GetLastN() int
	GetBackN() int
	GetRawOutput() bool
	GetConfig() *config.Config
}

// LLMDependencyProvider provides access to LLM-related services.
type LLMDependencyProvider interface {
	GetGateway() llm.LLMGateway
	GetPricingOverrides() map[string]pricing.ModelPricing
	GetTracker() pricing.CostTracker
	GetPricingData() pricing.PricingData
}

// PersistenceDependencyProvider provides access to history and persistence paths.
type PersistenceDependencyProvider interface {
	GetHistoryManager() HistoryManager
	GetPaths() *persistence.Paths
}

// InfrastructureDependencyProvider provides access to cross-cutting infrastructure.
type InfrastructureDependencyProvider interface {
	GetRegistry() tools.Registry
	GetSecurityManager() security.Manager
	GetEventBus() events.EventBus
	GetLogger() *slog.Logger
	GetSessionProvider() SessionProvider
}

// SessionDependencies defines the dependencies required for a session.
type SessionDependencies interface {
	LLMDependencyProvider
	PersistenceDependencyProvider
	InfrastructureDependencyProvider
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
