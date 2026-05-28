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

const (
	// DefaultShutdownTimeout defines the standard grace period for application cleanup and flushing
	// asynchronous buffers (e.g., suggestion persistence, telemetry events).
	DefaultShutdownTimeout = 1 * time.Second
)

// Session encapsulates the state of a single conversation session.
type Session struct {
	// ID is the unique identifier for this session, typically a UUID.
	ID string
	// StartTime records when the session was created.
	StartTime time.Time
	// History provides access to the conversation's persisted message history.
	History HistoryManager
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
	// Chat executes a single conversation turn. It blocks until the turn
	// completes (including all tool calls) or the context is cancelled.
	// The prompt is the user's input text; the session provides history
	// and configuration context.
	Chat(ctx context.Context, s *Session, prompt string) error

	// Shutdown initiates graceful shutdown of the chat executor. It blocks
	// until all in-flight operations complete or the context is cancelled.
	// After Shutdown returns, no further Chat calls should be made.
	Shutdown(ctx context.Context) error
}

// ChatConfigurator defines the interface for configuring chat parameters.
type ChatConfigurator interface {
	// SetLimits configures the maximum number of tool-calling turns,
	// the history token budget, and the history turn budget.
	// A value of 0 for any parameter means "no limit" (or the system default).
	// This method is safe to call between Chat invocations.
	SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error
}

// ChatEventSource defines the interface for subscribing to chat events.
type ChatEventSource interface {
	// Subscribe registers a callback that receives all chat events published
	// by this Chatter. The callback is invoked synchronously on the event
	// publisher's goroutine. Callers must not block inside the callback.
	// Multiple subscribers are supported; the order of delivery is undefined.
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
	// ProviderName identifies the LLM provider (e.g., "openai", "anthropic", "google").
	ProviderName string
	// Model specifies the LLM model name (e.g., "gpt-5", "claude-3-5-sonnet").
	Model string
	// Mode selects the agent personality/mode (e.g., "architect", "coder", "reviewer").
	Mode string
	// LogPath is the filesystem path for writing structured chat logs.
	LogPath string
	// TracePath is the filesystem path for writing detailed execution traces.
	TracePath string
}

// ChatterFactory defines the functional signature for creating a Chatter instance.
type ChatterFactory func(ctx context.Context, deps SessionDependencies, cfg ChatterConfig) (Chatter, error)

// SessionConfig defines the configuration interface for a session.
type SessionConfig interface {
	// GetPrompt returns the user's input prompt for the current turn.
	GetPrompt() string

	// GetLastN returns the number of recent history entries to display.
	GetLastN() int

	// GetBackN returns the number of turns to roll back when processing
	// a history navigation command.
	GetBackN() int

	// GetRawOutput returns true if markdown rendering should be disabled
	// and output should be displayed as plain text.
	GetRawOutput() bool

	// GetConfig returns the full application configuration.
	GetConfig() *config.Config
}

// LLMDependencyProvider provides access to LLM-related services.
type LLMDependencyProvider interface {
	// GetGateway returns the LLM gateway used for generating responses.
	GetGateway() llm.LLMGateway

	// GetPricingOverrides returns user-configured pricing overrides
	// that supplement or replace the built-in pricing data.
	GetPricingOverrides() map[string]pricing.ModelPricing

	// GetTracker returns the cost tracker for monitoring cumulative
	// token usage and estimated spend.
	GetTracker() pricing.CostTracker

	// GetPricingData returns the built-in pricing data for all known models.
	GetPricingData() pricing.PricingData
}

// PersistenceDependencyProvider provides access to history and persistence paths.
type PersistenceDependencyProvider interface {
	GetHistoryManager() HistoryManager
	GetPaths() *persistence.Paths
}

// InfrastructureDependencyProvider provides access to cross-cutting infrastructure.
type InfrastructureDependencyProvider interface {
	GetRegistry() (tools.Registry, error)
	GetSecurityManager() security.Manager
	GetEventBus() events.EventBus
	GetLogger() Logger
	GetTurnsLogger() TurnsLogger
	GetSessionProvider() SessionProvider
	GetHealthManager() HealthCheckManager
}

// SessionDependencies defines the dependencies required for a session.
type SessionDependencies interface {
	LLMDependencyProvider
	PersistenceDependencyProvider
	InfrastructureDependencyProvider
}

// ContextMetadata contains diagnostics and auxiliary data produced by the
// context preparation pipeline (pruning, summarization, truncation).
//
// Consumers receive this via ContextRequest and should treat it as
// read-only except for Warnings (which may be appended to).
type ContextMetadata struct {
	// OriginalTokenCount is the token count of the full history before any
	// pruning or summarization was applied.
	OriginalTokenCount int
	// FinalTokenCount is the token count after all transformations.
	FinalTokenCount int
	// FinalTurnCount is the number of conversation turns retained after pruning.
	FinalTurnCount int
	// PrunedTurns is the number of turns removed by pruning policies.
	PrunedTurns int
	// SummarizedTurns is the number of turns replaced by a summary.
	SummarizedTurns int
	// SummarizationAttempted is true if the summarizer was invoked,
	// regardless of whether it succeeded.
	SummarizationAttempted bool
	// MaintenanceBlocked is true if context maintenance was skipped
	// (e.g., because the conversation is too short to benefit).
	MaintenanceBlocked bool
	// Warnings collects non-fatal diagnostic messages from the pipeline.
	// Consumers may append to this slice.
	Warnings []string
	// TotalTurnsKept is the total number of turns retained across all
	// retention policies.
	TotalTurnsKept int
	// KeptByPolicy maps each pruning policy name to the number of turns
	// that policy elected to keep.
	KeptByPolicy map[string]int
	// History is the post-transform content slice that will be sent to
	// the LLM. Callers must Clone before mutating.
	History []*llm.Content
}

// ContextRequest represents the input and state of a context preparation pipeline.
type ContextRequest struct {
	// Turn is the zero-based index of the current conversation turn.
	Turn int
	// History is the raw, pre-transform conversation history.
	// Transformers may replace or truncate this slice.
	History []*llm.Content
	// Metadata accumulates diagnostics as the request flows through the
	// transformer chain. Transformers should update relevant fields.
	Metadata ContextMetadata
	// PersistHistory indicates whether the final History should be
	// persisted after the pipeline completes. Set by the orchestrator.
	PersistHistory bool
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	// MarkTurns evaluates each turn group and sets the corresponding
	// keep[i] to true if the turn should be retained. It returns the
	// number of turns marked for retention. A non-nil error aborts the
	// pruning pipeline.
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error)

	// Name returns a human-readable identifier for this policy,
	// used in ContextMetadata.KeptByPolicy and diagnostic logs.
	Name() string
}

// ContextTransformer modifies the context before it's sent to the LLM.
// Transformers are applied in ascending Priority order.
type ContextTransformer interface {
	// Transform applies a mutation to the ContextRequest. Common
	// transformations include summarization, truncation, and pruning.
	// Implementations must be safe for single-goroutine sequential use;
	// concurrent invocation is not required.
	Transform(ctx context.Context, req *ContextRequest) error

	// Priority returns the execution order of this transformer.
	// Lower values execute first. Values need not be contiguous.
	Priority() int
}

// ResultStrategy defines how tool outputs are transformed back into LLM messages.
type ResultStrategy interface {
	// Format converts a tool execution result into an LLM-compatible Part.
	// The call parameter provides the original function invocation context;
	// the result parameter contains the tool's output.
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
