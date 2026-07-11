// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// GuardStep validates the Turn against limits before proceeding.
type GuardStep struct{}

func (p *GuardStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}

	maxTurns := Turn.CtxManager.GetLimits().MaxToolTurns
	if Turn.Index > maxTurns {
		return ProcessResult{}, NewAgentError(llm.ErrTerminal, fmt.Sprintf("turn %d exceeds limit %d", Turn.Index, maxTurns), llm.ErrMaxTurnsReached)
	}

	// Freeze session turn count before persistence mutates history.
	// This value is used by TurnStatusEvent throughout the turn lifecycle.
	Turn.SessionTurnsAtStart = Turn.CtxManager.History.GetTotalEntries() / 2

	evt := events.TurnStarted{
		Turn:         Turn.Index,
		SessionTurns: Turn.SessionTurnsAtStart,
		MaxTurns:     maxTurns,
	}
	if err := events.SafePublish(ctx, Turn.Events, evt); err != nil {
		if errors.Is(err, events.ErrBusNotInitialized) {
			return ProcessResult{NextPhase: PhaseRefining}, nil
		}
		Turn.getLogger().Error("event_publish_failed",
			"event_type", string(evt.Type()),
			"error", err)
		return ProcessResult{}, err
	}
	return ProcessResult{NextPhase: PhaseRefining}, nil
}

// ContextRefiner prepares the context for the LLM call.
type ContextRefiner struct{}

func (p *ContextRefiner) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	history, metadata, err := Turn.CtxManager.Prepare(ctx, Turn.Index)
	if err != nil {
		category := llm.ErrTerminal
		if IsTransient(err) {
			category = llm.ErrTransient
		}
		return ProcessResult{}, NewAgentError(category, "context preparation failed", err)
	}
	Turn.State.Metadata = metadata
	Turn.State.Tokens = metadata.FinalTokenCount
	Turn.State.PreparedHistory = history

	return ProcessResult{NextPhase: PhaseInference}, nil
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	if Turn.State.Response != nil {
		if err := Turn.CtxManager.AddContent(ctx, Turn.State.Response); err != nil {
			category := llm.ErrTerminal
			if IsTransient(err) {
				category = llm.ErrTransient
			}
			return ProcessResult{}, NewAgentError(category, "history error", err)
		}
	}

	if Turn.State.ToolResponse != nil {
		if err := Turn.CtxManager.AddContent(ctx, Turn.State.ToolResponse); err != nil {
			category := llm.ErrTerminal
			if IsTransient(err) {
				category = llm.ErrTransient
			}
			return ProcessResult{}, NewAgentError(category, "failed to persist tool results", err)
		}
	}

	return ProcessResult{NextPhase: PhaseComplete}, nil
}

// RecoveryStep handles errors by deciding whether to retry or fail.
type RecoveryStep struct {
	Policy RetryPolicy
}

func (p *RecoveryStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	err := Turn.State.LastError
	if err == nil {
		return ProcessResult{NextPhase: PhaseComplete}, nil
	}

	category := llm.ClassifyLLMError(err)

	switch category {
	case llm.LLMErrorRateLimited:
		Turn.State.HasSeenRateLimit = true
	case llm.LLMErrorAuthFailure:
		// Auth failures are non-retryable — surface immediately
		return ProcessResult{NextPhase: PhaseComplete}, err
	case llm.LLMErrorContextOverflow:
		// Context overflow triggers re-assembly with summarisation,
		// not a blind retry. Only attempt one refinement per turn
		// to prevent infinite loops when summarisation doesn't help.
		Turn.getLogger().Warn("context_overflow_detected",
			"error", err,
			"turn", Turn.Index)
		if Turn.State.RetryCount == 0 {
			Turn.State.RetryCount++
			return ProcessResult{NextPhase: PhaseRefining}, nil
		}
		// Subsequent context overflows fall through to retry/failure
	case llm.LLMErrorTimeout, llm.LLMErrorServerError:
		// Fall through to retry logic below
	default:
		// Architect-acceptance (2026-07): ClassifyLLMError returns exactly one of
		// the five known categories (RateLimited, ContextOverflow, AuthFailure,
		// ServerError, Timeout). The default case is a defensive guard that is
		// structurally unreachable. Same acceptance class as json.Marshal on
		// all-string structs in global_prompt_tracker.go.
		// See: docs/architect/INTENTIONAL_NON_FIXES.md.
		// Unknown category — fall through to retry logic
	}

	delay, retry := p.Policy.ShouldRetry(Turn.Clock, err, Turn.State.RetryCount, Turn.State.HasSeenRateLimit)
	if !retry {
		return p.handleFailure(err)
	}

	return p.attemptRetry(ctx, Turn, delay)
}

func (p *RecoveryStep) handleFailure(err error) (ProcessResult, error) {
	if IsTransient(err) {
		return ProcessResult{NextPhase: PhaseComplete}, fmt.Errorf("max retries reached: %w", err)
	}
	return ProcessResult{NextPhase: PhaseComplete}, err
}

func (p *RecoveryStep) attemptRetry(ctx context.Context, Turn *Turn, delay time.Duration) (ProcessResult, error) {
	Turn.State.RetryCount++

	// Log retry to application logs (Technical debugging only)
	Turn.getLogger().Debug("retrying_after_transient_error",
		"error", Turn.State.LastError,
		"delay", delay,
		"attempt", Turn.State.RetryCount)

	// Publish retry notification to the UI/EventBus
	msg := fmt.Sprintf("Transient error: %v. Retrying in %v (Attempt %d)...",
		Turn.State.LastError, delay.Round(time.Millisecond), Turn.State.RetryCount)
	evt := events.SystemMessageEvent{
		Message: msg,
		Level:   "warn",
	}
	if err := events.SafePublish(ctx, Turn.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			Turn.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
			return ProcessResult{}, err
		}
	}

	// Publish RetryWaitingEvent to show the spinner during the backoff delay
	_ = events.SafePublish(ctx, Turn.Events, events.RetryWaitingEvent{Duration: delay})

	// Coverage: defensive guard — ctx cancellation between event publish and select is caught by the select:case itself; this early-check handles the interleaving edge case.
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}

	select {
	case <-ctx.Done():
		return ProcessResult{}, ctx.Err()
	case <-Turn.Clock.After(delay):
	}

	return ProcessResult{NextPhase: PhaseRefining}, nil
}
