// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// tokenEstimator interface defines the method for estimating tokens.
type tokenEstimator interface {
	estimateTokens(contents []*llm.Content) int
}

// tokenGatekeeper estimates tokens and triggers auto-summarization if needed.
type tokenGatekeeper struct {
	MaxTokens   int
	Estimator   tokenEstimator
	Summarizer  ports.Summarizer
	Events      events.EventBus
	Strategies  map[string]ThresholdStrategy
	DefaultTier string
	Logger      *slog.Logger
}

func (t *tokenGatekeeper) getStrategy() ThresholdStrategy {
	if t.Strategies != nil {
		if s, ok := t.Strategies[t.DefaultTier]; ok {
			return s
		}
	}
	return &dynamicThresholdStrategy{Estimator: t.Estimator}
}

func (t *tokenGatekeeper) Transform(ctx context.Context, req *ports.ContextRequest) error {
	// 0. Domain Boundary Validation: Ensure history is structurally sound before processing
	if _, err := groupTurns(ctx, req.History); err != nil {
		return fmt.Errorf("gatekeeper validation failed: %w", err)
	}

	// Stage 1: Initial Analysis (includes Tiered Threshold)
	tokens, err := t.handleTieredThreshold(ctx, req)
	if err != nil {
		return err
	}

	// Stage 2: Pressure Management (90% threshold)
	tokens, err = t.handleSafetyPressure(ctx, req, tokens)
	if err != nil {
		return err
	}

	// Stage 3: Boundary Validation (Hard limits + Buffer)
	if err := t.validateHardLimits(ctx, req, tokens); err != nil {
		return err
	}

	req.Metadata.FinalTokenCount = tokens
	return nil
}

func (t *tokenGatekeeper) handleTieredThreshold(ctx context.Context, req *ports.ContextRequest) (int, error) {
	tokens := t.Estimator.estimateTokens(req.History)
	req.Metadata.OriginalTokenCount = tokens

	strategy := t.getStrategy()
	if strategy.Evaluate(tokens) {
		return t.triggerSummarization(ctx, req, tokens, strategy.GetThreshold(), "High-tier pricing threshold reached")
	}
	return tokens, nil
}

func (t *tokenGatekeeper) handleSafetyPressure(ctx context.Context, req *ports.ContextRequest, tokens int) (int, error) {
	if t.MaxTokens > 0 && tokens > int(float64(t.MaxTokens)*0.9) {
		return t.triggerSummarization(ctx, req, tokens, t.MaxTokens, "Safety limit pressure (> 90%)")
	}
	return tokens, nil
}

func (t *tokenGatekeeper) triggerSummarization(ctx context.Context, req *ports.ContextRequest, tokens, limit int, reason string) (int, error) {
	if t.Events != nil {
		evt := events.SummarizationRequired{
			Tokens:   tokens,
			MaxLimit: limit,
			Reason:   reason,
		}
		if err := events.SafePublish(ctx, t.Events, evt); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				t.getLogger().Error("event_publish_failed",
					slog.String("event_type", string(evt.Type())),
					slog.Any("error", err))
				return tokens, err
			}
		}
	}

	if req.Metadata.SummarizationAttempted {
		return tokens, nil
	}

	n, err := t.autoSummarize(ctx, req)
	if err != nil {
		// Propagate critical errors, but continue if blocked
		if errors.Is(err, errInvalidPayload) {
			return tokens, err
		}
		if req.Metadata.MaintenanceBlocked || len(req.History) < 10 {
			return tokens, nil
		}
		return tokens, err
	}

	newTokens := t.Estimator.estimateTokens(req.History)
	req.Metadata.SummarizedTurns = n
	return newTokens, nil
}

func (t *tokenGatekeeper) validateHardLimits(ctx context.Context, req *ports.ContextRequest, tokens int) error {
	if t.MaxTokens <= 0 {
		return nil
	}

	limit := t.MaxTokens
	reserved := config.SystemContextBuffer
	// Ensure we don't reserve so much space that the agent becomes unusable in small contexts.
	// We reserve up to 10% of the context for system overhead, capped at the SystemContextBuffer.
	maxReserved := int(float64(t.MaxTokens) * 0.1)
	if reserved > maxReserved {
		reserved = maxReserved
	}
	limit -= reserved

	if tokens > limit {
		if t.Events != nil {
			e1 := events.TokenLimitReachedEvent{
				Tokens:   tokens,
				MaxLimit: t.MaxTokens,
			}
			if err := events.SafePublish(ctx, t.Events, e1); err != nil {
				if !errors.Is(err, events.ErrBusNotInitialized) {
					t.getLogger().Error("event_publish_failed",
						slog.String("event_type", string(e1.Type())),
						slog.Any("error", err))
				}
			}

			e2 := events.SystemMessageEvent{
				Message: fmt.Sprintf("Payload estimate (%d tokens) exceeds safety limit (%d) including system overhead buffer!", tokens, limit),
				Level:   "error",
			}
			if err := events.SafePublish(ctx, t.Events, e2); err != nil {
				if !errors.Is(err, events.ErrBusNotInitialized) {
					t.getLogger().Error("event_publish_failed",
						slog.String("event_type", string(e2.Type())),
						slog.Any("error", err))
				}
			}
		}
		return llm.ErrContextLimitExceeded
	}

	return nil
}

func (t *tokenGatekeeper) Priority() int { return 80 }

func (t *tokenGatekeeper) autoSummarize(ctx context.Context, req *ports.ContextRequest) (int, error) {
	if len(req.History) < 10 {
		req.Metadata.MaintenanceBlocked = true
		return 0, fmt.Errorf("not enough history to auto-summarize (got %d)", len(req.History))
	}

	// 1. Locate the block
	start, end, numTurns, err := t.findSummarizableRange(ctx, req.History)
	if err != nil {
		req.Metadata.MaintenanceBlocked = true
		return 0, err
	}

	// 2. Logging
	if t.Events != nil {
		subsetTokens := t.Estimator.estimateTokens(req.History[start:end])
		evt := events.SystemMessageEvent{
			Message: fmt.Sprintf("Auto-summarizing %d turns in range [%d:%d] (~%d tokens) due to context pressure...", numTurns, start, end, subsetTokens),
			Level:   "info",
		}
		if err := events.SafePublish(ctx, t.Events, evt); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				t.getLogger().Error("event_publish_failed",
					slog.String("event_type", string(evt.Type())),
					slog.Any("error", err))
			}
		}
	}

	// 3. Service Call
	if t.Summarizer == nil {
		req.Metadata.MaintenanceBlocked = true
		return 0, fmt.Errorf("%w: summarizer not initialized; cannot perform auto-summarization", llm.ErrTerminal)
	}

	summary, _, err := t.Summarizer.Summarize(ctx, req.History[start:end], "")
	if err != nil {
		return 0, err
	}

	// Signal completion to the UI
	if t.Events != nil {
		evt := events.SystemMessageEvent{
			Message: "Auto-summarization complete. Context successfully compressed.",
			Level:   "info",
		}
		if err := events.SafePublish(ctx, t.Events, evt); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				t.getLogger().Error("event_publish_failed",
					slog.String("event_type", string(evt.Type())),
					slog.Any("error", err))
			}
		}
	}

	// 4. State Mutation
	req.History = applySummaryToHistory(req.History, start, end, summary)
	req.Metadata.SummarizationAttempted = true
	req.PersistHistory = true
	return numTurns, nil
}

func (t *tokenGatekeeper) findSummarizableRange(ctx context.Context, history []*llm.Content) (int, int, int, error) {
	turns, err := groupTurns(ctx, history)
	if err != nil {
		return 0, 0, 0, err
	}

	// We want to summarize about 50% of the history, but at least 2 turns.
	targetTurns := len(turns) / 2
	if targetTurns < 2 {
		targetTurns = 2
	}

	startTurn, numTurns := t.locateCandidateBlock(ctx, turns, targetTurns)

	if startTurn == -1 || numTurns < 2 {
		return 0, 0, 0, fmt.Errorf("could not find a contiguous block of at least 2 unpinned turns to summarize")
	}

	// Calculate message offsets
	startIdx := t.countMessages(turns[:startTurn])
	endIdx := startIdx + t.countMessages(turns[startTurn:startTurn+numTurns])

	return startIdx, endIdx, numTurns, nil
}

func (t *tokenGatekeeper) locateCandidateBlock(ctx context.Context, turns [][]*llm.Content, target int) (int, int) {
	startTurn := -1
	numTurns := 0

	for i := 0; i < len(turns); i++ {
		// SCALABLE: Periodic context check in potentially long loops
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return -1, 0
			default:
			}
		}

		if !t.isTurnPinned(turns[i]) {
			if startTurn == -1 {
				startTurn = i
			}
			numTurns++
			if numTurns >= target {
				return startTurn, numTurns
			}
		} else {
			// If we found a pinned turn and we haven't reached target, but have a viable block, use it.
			if numTurns >= 2 {
				return startTurn, numTurns
			}
			startTurn = -1
			numTurns = 0
		}
	}

	return startTurn, numTurns
}

func (t *tokenGatekeeper) countMessages(turns [][]*llm.Content) int {
	count := 0
	for _, turn := range turns {
		count += len(turn)
	}
	return count
}

func (t *tokenGatekeeper) isTurnPinned(turn []*llm.Content) bool {
	for _, msg := range turn {
		if msg.Pinned {
			return true
		}
	}
	return false
}

func applySummaryToHistory(history []*llm.Content, start, end int, summary string) []*llm.Content {
	updated := make([]*llm.Content, 0, len(history)-(end-start)+2)
	updated = append(updated, history[:start]...)

	sumUser := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "System Auto-Summary (context limit reached):\n\n" + summary}},
	}
	sumModel := &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: "Understood. Context compressed."}},
	}

	// Handle role alternation at the start of the injection
	if len(updated) > 0 && updated[len(updated)-1].Role == "user" {
		last := updated[len(updated)-1]
		cloned := llm.CloneContent(last)
		cloned.Parts = append(cloned.Parts, &llm.Part{Text: "\n\n" + sumUser.Parts[0].Text})
		updated[len(updated)-1] = cloned
		updated = append(updated, sumModel)
	} else {
		updated = append(updated, sumUser, sumModel)
	}

	// Handle role alternation at the end of the injection
	remainder := history[end:]
	if len(remainder) > 0 && remainder[0].Role == "model" {
		first := remainder[0]
		cloned := llm.CloneContent(first)
		// Prepend acknowledgment text
		cloned.Parts = append([]*llm.Part{{Text: sumModel.Parts[0].Text + "\n\n"}}, cloned.Parts...)

		// If we just appended sumModel in the previous step, we now have:
		// [..., sumModel, cloned(model)] which is still consecutive.
		// So we should replace the last sumModel we just added with cloned.
		updated[len(updated)-1] = cloned
		updated = append(updated, remainder[1:]...)
	} else {
		updated = append(updated, remainder...)
	}

	return updated
}

func (t *tokenGatekeeper) getLogger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}
