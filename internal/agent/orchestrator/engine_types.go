// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"fmt"
	"time"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// RuntimeConfig defines the runtime configuration for the Engine.
type RuntimeConfig struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
}

// validate checks that the RuntimeConfig contains the minimum viable
// fields required by the orchestrator Engine. It is called by
// Engine.Reconfigure to fail fast on malformed runtime configuration.
// See ADR-029.
//
// Rules (minimum viable set per ADR-029 §6):
//   - ProviderName must be non-empty.
//   - Model must be non-empty.
//   - Mode must be non-empty.
//   - PricingOverrides may be nil or empty; if non-empty, no key may be the empty string.
//
// Additional validation rules will be added in separate, individually-tracked issues
// per ADR-029 negative-consequence mitigation. Do not extend this method ad-hoc.
func (c RuntimeConfig) validate() error {
	if c.ProviderName == "" {
		return fmt.Errorf("runtime config: provider name must not be empty")
	}
	if c.Model == "" {
		return fmt.Errorf("runtime config: model must not be empty")
	}
	if c.Mode == "" {
		return fmt.Errorf("runtime config: mode must not be empty")
	}
	for k := range c.PricingOverrides {
		if k == "" {
			return fmt.Errorf("runtime config: pricing overrides contain empty key")
		}
	}
	return nil
}

// EngineConfigSnapshot is a value-type snapshot of the user-facing
// engine configuration fields. It is returned by Engine.GetConfig()
// for diagnostic use (drift detection). Not for hot-path use.
type EngineConfigSnapshot struct {
	ProviderName string
	Model        string
	Mode         string
}

// engineConfig defines the lock-free runtime state for the Engine.
type engineConfig struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
	CostTracker      domain_pricing.CostTracker
	SM               domain_security.Manager
	Logger           ports.Logger
}

// TurnPhase represents the current stage of a single agent Turn.
type TurnPhase string

const (
	PhaseGuard      TurnPhase = "Guard"
	PhaseRefining   TurnPhase = "Refining"
	PhaseInference  TurnPhase = "Inference"
	PhaseExecuting  TurnPhase = "Executing"
	PhasePersisting TurnPhase = "Persisting"
	PhaseRecovering TurnPhase = "Recovering"
	PhaseComplete   TurnPhase = "Complete"
)

// ProcessResult describes the outcome of a phase execution.
type ProcessResult struct {
	NextPhase TurnPhase
	Stop      bool // Explicit signal to halt the Turn
	Recovery  bool // Explicit signal that we should enter recovery
}

// RetryPolicy defines how the engine should handle errors and retries.
type RetryPolicy interface {
	ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool)
}

// DefaultRetryPolicy provides a standard retry implementation with exponential backoff and jitter.
type DefaultRetryPolicy struct {
	MaxRetries       int
	Backoff          time.Duration
	RateLimitBackoff time.Duration
}

// calculateBackoff computes the exponential backoff delay, doubling per attempt
// and capping at maxDelay. It is a pure function with no side effects, designed
// for exhaustive table-driven testing independent of error classification or Clock.
func calculateBackoff(base time.Duration, attempt int) time.Duration {
	const maxDelay = 2 * time.Minute

	if base >= maxDelay {
		return maxDelay
	}

	delay := base
	for i := 0; i < attempt; i++ {
		if delay >= maxDelay/2 {
			return maxDelay
		}
		delay *= 2
	}
	return delay
}

func (p *DefaultRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool) {
	if attempt >= p.MaxRetries {
		return 0, false
	}
	if IsFatal(err) {
		return 0, false
	}
	if IsTransient(err) {
		base := p.Backoff

		// Use the severe backoff if we have been rate-limited at any point during this Turn's
		// retry sequence, to avoid "flooding" the provider again.
		if hasSeenRateLimit {
			base = p.RateLimitBackoff
		}

		delay := calculateBackoff(base, attempt)

		// Apply Jitter
		finalDelay := time.Duration(c.Jitter(float64(delay)))

		return finalDelay, true
	}
	return 0, false
}

// TurnHook allows intercepting lifecycle events of a Turn.
type TurnHook interface {
	BeforeTurn(Turn *Turn)
	AfterTurn(Turn *Turn, err error)
	OnPhaseTransition(from, to TurnPhase, state *TurnState)
}

// TurnState carries data between the phases of a Turn and tracks the current phase.
type TurnState struct {
	Phase                TurnPhase         `json:"phase"`
	HasToolCalls         bool              `json:"has_tool_calls"`
	Metrics              *llm.Metrics      `json:"metrics,omitempty"`
	Tokens               int               `json:"tokens"`
	CurrentTurns         int               `json:"current_turns"`
	Metadata             *sessctx.Metadata `json:"metadata,omitempty"`
	Response             *llm.Content      `json:"response,omitempty"`
	ToolResponse         *llm.Content      `json:"tool_response,omitempty"`
	LastError            error             `json:"-"`
	RetryCount           int               `json:"retry_count"`
	HasSeenRateLimit     bool              `json:"-"`
	ToolCallCount        map[string]int    `json:"-"`
	RecentResponseHashes []string          `json:"-"`
	PreparedHistory      []*llm.Content    `json:"-"`
	TaskCost             float64           `json:"task_cost"`
	ToolReasons          []string          `json:"-"`
}

// ToolExecutor defines the interface for tool execution.
type ToolExecutor interface {
	Execute(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error)
}

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, Turn *Turn) (ProcessResult, error)
}

// TurnProcessorFunc is an adapter to allow the use of ordinary functions as TurnProcessors.
type TurnProcessorFunc func(context.Context, *Turn) (ProcessResult, error)

// Process calls f(ctx, Turn).
func (f TurnProcessorFunc) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	return f(ctx, Turn)
}

// turnMiddleware wraps a TurnProcessor to inject cross-cutting concerns.
type turnMiddleware func(TurnProcessor) TurnProcessor
