// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

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

// TokenEstimator interface defines the method for estimating tokens.
type TokenEstimator interface {
	EstimateTokens(contents []*llm.Content) int
}

// TokenGatekeeper estimates tokens and triggers auto-summarization if needed.
type TokenGatekeeper struct {
	MaxTokens         int
	Estimator         TokenEstimator
	Summarizer        ports.Summarizer
	Events            events.EventBus
	Logger            ports.Logger
	CandidateSelector CandidateSelector
}

func (t *TokenGatekeeper) Transform(ctx context.Context, req *ports.ContextRequest) error {
	// 0. Domain Boundary Validation: Ensure history is structurally sound before processing
	if _, err := groupTurns(ctx, req.History); err != nil {
		return fmt.Errorf("gatekeeper validation failed: %w", err)
	}

	tokens := t.Estimator.EstimateTokens(req.History)
	req.Metadata.OriginalTokenCount = tokens

	// Stage 1: Pressure Management (90% threshold)
	tokens, err := t.handleSafetyPressure(ctx, req, tokens)
	if err != nil {
		return err
	}

	// Stage 2: Boundary Validation (Hard limits + Buffer)
	if err := t.validateHardLimits(ctx, req, tokens); err != nil {
		return err
	}

	req.Metadata.FinalTokenCount = tokens
	return nil
}

func (t *TokenGatekeeper) handleSafetyPressure(ctx context.Context, req *ports.ContextRequest, tokens int) (int, error) {
	if t.MaxTokens > 0 && tokens > int(float64(t.MaxTokens)*0.9) {
		return t.triggerSummarization(ctx, req, tokens, t.MaxTokens, "safety limit pressure (> 90%)")
	}
	return tokens, nil
}

func (t *TokenGatekeeper) triggerSummarization(ctx context.Context, req *ports.ContextRequest, tokens, limit int, reason string) (int, error) {
	if err := t.publishSummarizationEvent(ctx, tokens, limit, reason); err != nil {
		return tokens, err
	}
	if req.Metadata.SummarizationAttempted {
		return tokens, nil
	}
	n, err := t.autoSummarize(ctx, req)
	if err != nil {
		if isBlockingError(err) {
			return tokens, err
		}
		if req.Metadata.MaintenanceBlocked || len(req.History) < 10 {
			return tokens, nil
		}
		return tokens, err
	}
	return t.applySummarizationResult(req, n), nil
}

func (t *TokenGatekeeper) validateHardLimits(ctx context.Context, req *ports.ContextRequest, tokens int) error {
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
						"event_type", string(e1.Type()),
						"error", err)
				}
			}

			e2 := events.SystemMessageEvent{
				Message: fmt.Sprintf("payload estimate (%d tokens) exceeds safety limit (%d) including system overhead buffer", tokens, limit),
				Level:   "error",
			}
			if err := events.SafePublish(ctx, t.Events, e2); err != nil {
				if !errors.Is(err, events.ErrBusNotInitialized) {
					t.getLogger().Error("event_publish_failed",
						"event_type", string(e2.Type()),
						"error", err)
				}
			}
		}
		return llm.ErrContextLimitExceeded
	}

	return nil
}

func (t *TokenGatekeeper) Priority() int { return 80 }

func (t *TokenGatekeeper) autoSummarize(ctx context.Context, req *ports.ContextRequest) (int, error) {
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
	subsetTokens := t.Estimator.EstimateTokens(req.History[start:end])
	msg := fmt.Sprintf("auto-summarizing %d turns in range [%d:%d] (~%d tokens) due to context pressure", numTurns, start, end, subsetTokens)
	t.publishSystemEvent(ctx, msg, "info")

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
	t.publishSystemEvent(ctx, "auto-summarization complete; context successfully compressed", "info")

	// 4. State Mutation
	req.History = applySummaryToHistory(req.History, start, end, summary)
	req.Metadata.SummarizationAttempted = true
	req.PersistHistory = true
	return numTurns, nil
}

func (t *TokenGatekeeper) publishSystemEvent(ctx context.Context, message, level string) {
	if t.Events == nil {
		return
	}
	evt := events.SystemMessageEvent{
		Message: message,
		Level:   level,
	}
	if err := events.SafePublish(ctx, t.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			t.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}

// publishSummarizationEvent emits a SummarizationRequired event on the bus
// following the same resilience pattern as publishSystemEvent but propagates
// non-initialization errors to the caller so that event-bus failures can
// halt the summarization pipeline.
func (t *TokenGatekeeper) publishSummarizationEvent(ctx context.Context, tokens, limit int, reason string) error {
	if t.Events == nil {
		return nil
	}
	evt := events.SummarizationRequired{
		Tokens:   tokens,
		MaxLimit: limit,
		Reason:   reason,
	}
	if err := events.SafePublish(ctx, t.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			t.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
			return err
		}
	}
	return nil
}

// isBlockingError returns true for errors that should halt the request
// pipeline rather than being silently swallowed.
func isBlockingError(err error) bool {
	return errors.Is(err, ErrInvalidPayload) || errors.Is(err, llm.ErrTerminal)
}

// applySummarizationResult updates the context request with the summarization
// outcome and returns the new token count.
func (t *TokenGatekeeper) applySummarizationResult(req *ports.ContextRequest, summarizedTurns int) int {
	newTokens := t.Estimator.EstimateTokens(req.History)
	req.Metadata.SummarizedTurns = summarizedTurns
	req.Metadata.SummarizationAttempted = true
	req.PersistHistory = true
	return newTokens
}

func (t *TokenGatekeeper) FindSummarizableRange(ctx context.Context, history []*llm.Content) (int, int, int, error) {
	return t.findSummarizableRange(ctx, history)
}

func (t *TokenGatekeeper) findSummarizableRange(ctx context.Context, history []*llm.Content) (int, int, int, error) {
	turns, err := groupTurns(ctx, history)
	if err != nil {
		return 0, 0, 0, err
	}

	if t.CandidateSelector == nil {
		t.CandidateSelector = &ContiguousUnpinnedSelector{}
	}
	minViable := t.CandidateSelector.MinViableBlock()

	// We want to summarize about 50% of the history, but at least minViable turns.
	targetTurns := len(turns) / 2
	if targetTurns < minViable {
		targetTurns = minViable
	}

	startTurn, numTurns := t.locateCandidateBlock(ctx, turns, targetTurns)

	if startTurn == -1 || numTurns < minViable {
		return 0, 0, 0, fmt.Errorf("could not find a contiguous block of at least %d turns to summarize", minViable)
	}

	// Calculate message offsets
	startIdx := t.countMessages(turns[:startTurn])
	endIdx := startIdx + t.countMessages(turns[startTurn:startTurn+numTurns])

	return startIdx, endIdx, numTurns, nil
}

func (t *TokenGatekeeper) locateCandidateBlock(ctx context.Context, turns [][]*llm.Content, target int) (int, int) {
	minViable := t.CandidateSelector.MinViableBlock()
	startTurn := -1
	numTurns := 0

	for i := 0; i < len(turns); i++ {
		if ctx.Err() != nil {
			return -1, 0
		}

		if !t.CandidateSelector.IsCandidate(turns[i]) {
			if numTurns >= minViable {
				return startTurn, numTurns
			}
			startTurn = -1
			numTurns = 0
			continue
		}

		if startTurn == -1 {
			startTurn = i
		}
		numTurns++
		if numTurns >= target {
			return startTurn, numTurns
		}
	}

	return startTurn, numTurns
}

func (t *TokenGatekeeper) countMessages(turns [][]*llm.Content) int {
	count := 0
	for _, turn := range turns {
		count += len(turn)
	}
	return count
}

func isTurnPinned(turn []*llm.Content) bool {
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
		Parts: []*llm.Part{{Text: "system auto-summary (context limit reached):\n\n" + summary}},
	}
	sumModel := &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: "understood: context compressed"}},
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

func (t *TokenGatekeeper) getLogger() ports.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}
