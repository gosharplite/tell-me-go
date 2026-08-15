// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"time"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
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
	// ConfigPath is the filesystem path to the main YAML configuration file.
	// Used by the config watcher to detect and apply runtime config changes.
	ConfigPath string
}

// ChatterFactory defines the functional signature for creating a Chatter instance.
type ChatterFactory func(ctx context.Context, deps ChatterComposer, cfg ChatterConfig) (Chatter, error)

// ChatterComposer defines the dependencies required to construct a Chatter
// via a ChatterFactory.
type ChatterComposer interface {
	GetGateway() llm.LLMGateway
	GetEventBus() events.EventBus
	GetPaths() *persistence.Paths
	GetHistoryManager() HistoryManager
	GetLogger() Logger
	GetTracker() pricing.CostTracker
	GetPricingOverrides() map[string]pricing.ModelPricing
	GetSessionProvider() SessionProvider
	GetTurnsLogger() TurnsLogger
	GetSecurityManager() security.Manager
	GetRegistry() (tools.Registry, error)
	// GetSkillRepository returns the shared skill repository. Must be non-nil.
	GetSkillRepository() domain_skills.SkillRepository
	// GetSummarizer returns the conversation history summarizer. Must be non-nil.
	GetSummarizer() Summarizer
	// GetConfigWatcher returns the runtime configuration watcher. May be nil (agent uses no-op fallback).
	GetConfigWatcher() domain_config.ConfigWatcher
	// RegisterTrace subscribes an execution trace recorder for the given file path.
	RegisterTrace(path string)
}

// SessionFinalizer defines the dependencies required to finalize a session
// (record costs, save history). It is the narrowest interface needed by
// Bootstrapper.FinalizeSession.
type SessionFinalizer interface {
	GetTracker() pricing.CostTracker
	GetPaths() *persistence.Paths
	GetPricingOverrides() map[string]pricing.ModelPricing
}
