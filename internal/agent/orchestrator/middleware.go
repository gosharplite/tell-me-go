// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

const LoopWarning = "SYSTEM WARNING: Infinite loop detected. You are repeating the exact same tool call or response. The previous action was aborted. Please analyze your previous steps and try a DIFFERENT tool or strategy."

// withStatusReporter returns a middleware that publishes Turn status events.
func (e *Engine) withStatusReporter() turnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, Turn *Turn) (ProcessResult, error) {
			// 1. Header: Trigger session boundary header at the absolute start of every LLM generation cycle.
			if Turn.State.Phase == PhaseInference && Turn.State.RetryCount == 0 && e.events != nil {
				e.publishTurnStatus(ctx, Turn, false, false)
			}

			res, err := next.Process(ctx, Turn)
			if e.events == nil || err != nil {
				return res, err
			}

			// 2. Metrics & Ready Footer:
			// If there are NO tool calls (final Turn), we still want to emit the metrics
			// But for tool call turns, we want to group Metrics and Ready at the absolute END
			// of the Turn (after tools have printed).
			if Turn.State.Phase == PhasePersisting {
				// We publish the Metrics line right before the Ready footer.
				if Turn.State.Metrics != nil {
					e.publishTurnStatus(ctx, Turn, true, false)
				}
				e.publishTurnStatus(ctx, Turn, false, true)
			}
			return res, nil
		})
	}
}

func (e *Engine) publishTurnStatus(ctx context.Context, Turn *Turn, isPostCall bool, isFinal bool) {
	// Always retrieve fresh limits from the context manager to ensure
	// autonomous turns (which reuse the same 'Turn' object) stay in sync with config.
	limits := Turn.CtxManager.GetLimits()
	maxTokens := limits.MaxHistoryTokens
	maxHistTurns := limits.MaxHistoryTurns
	threshold := limits.TieredThreshold

	var cost float64
	var dailyCost float64
	var totalM, totalH, totalO int64
	if Turn.CostTracker != nil {
		cost = Turn.CostTracker.GetTotalCost(ctx)
		dailyCost = Turn.CostTracker.GetDailyCost(ctx)
		stats, _ := Turn.CostTracker.GetStats(ctx)
		totalM = stats.PromptTokens - stats.CachedTokens
		totalH = stats.CachedTokens
		totalO = stats.ResponseTokens + stats.ThinkingTokens
	}

	evt := events.TurnStatusEvent{
		Status: events.TurnStatus{
			Timestamp:        Turn.Clock.Now(),
			CurrentTurns:     Turn.Index,
			SessionTurns:     Turn.CtxManager.History.GetTotalEntries() / 2,
			MaxHistoryTurns:  maxHistTurns,
			Tokens:           Turn.State.Tokens,
			MaxHistoryTokens: maxTokens,
			TieredThreshold:  threshold,
			Metrics:          Turn.State.Metrics,
			IsPostCall:       isPostCall,
			IsFinal:          isFinal,
			StartTime:        Turn.StartTime,
			SessionCost:      cost,
			DailyCost:        dailyCost,
			TaskCost:         Turn.State.TaskCost,
			TotalM:           totalM,
			TotalH:           totalH,
			TotalO:           totalO,
			ToolReasons:      Turn.State.ToolReasons,
			Mode:             Turn.Mode,
		},
	}
	if err := events.SafePublish(ctx, e.events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			e.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}

// withMetrics returns a middleware that publishes usage metrics.
func (e *Engine) withMetrics() turnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, Turn *Turn) (ProcessResult, error) {
			phase := Turn.State.Phase
			res, err := next.Process(ctx, Turn)

			// Trigger accumulation and event publication immediately after inference
			// so that subsequent status events (metrics line) have accurate data.
			if phase == PhaseInference && e.events != nil && err == nil && Turn.State.Metrics != nil {
				if Turn.CostTracker != nil {
					// Calculate and accumulate into session total (thread-safe)
					turnCost := Turn.CostTracker.AccumulateAndReturn(*Turn.State.Metrics)
					Turn.State.Metrics.Cost = turnCost
					Turn.State.TaskCost += turnCost
				}

				evt := events.UsageMetricsEvent{
					Context:   ctx,
					Metrics:   Turn.State.Metrics,
					StartTime: Turn.StartTime,
				}
				if err := events.SafePublish(ctx, e.events, evt); err != nil {
					if !errors.Is(err, events.ErrBusNotInitialized) {
						e.getLogger().Error("event_publish_failed",
							"event_type", string(evt.Type()),
							"error", err)
					}
				}
			}
			return res, err
		})
	}
}

// withLoopDetector returns a middleware that detects and breaks infinite tool loops.
func withLoopDetector() turnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, Turn *Turn) (ProcessResult, error) {
			res, err := next.Process(ctx, Turn)
			if Turn.State.Phase != PhaseInference || err != nil || Turn.State.Response == nil {
				return res, err
			}

			if detectLoop(Turn.State) {
				return handleLoopBreak(ctx, Turn)
			}

			return res, err
		})
	}
}

func detectLoop(state *TurnState) bool {
	// 1. Multi-step loop detection (Text & Tool Calls)
	rawJSON, _ := json.Marshal(state.Response)
	h := sha256.Sum256(rawJSON)
	currentHash := hex.EncodeToString(h[:])

	if isDuplicateResponse(currentHash, state.RecentResponseHashes) {
		return true
	}

	// Keep last N hashes (using the same repetition limit)
	state.RecentResponseHashes = append(state.RecentResponseHashes, currentHash)
	if len(state.RecentResponseHashes) > domain_config.DefaultMaxLoopRepetitions {
		state.RecentResponseHashes = state.RecentResponseHashes[1:]
	}

	// 2. Tool call loop detection (Immediate threshold)
	for _, p := range state.Response.Parts {
		if p.FunctionCall != nil {
			args, _ := json.Marshal(p.FunctionCall.Args)
			key := p.FunctionCall.Name + ":" + string(args)
			state.ToolCallCount[key]++
			if state.ToolCallCount[key] > domain_config.DefaultMaxLoopRepetitions {
				return true
			}
		}
	}

	return false
}

func handleLoopBreak(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	// Publish warning event for UI visibility
	_ = events.SafePublish(ctx, Turn.Events, events.SystemMessageEvent{
		Message: "Infinite loop detected! Injecting corrective feedback to break the cycle...",
		Level:   "warn",
	})

	// SCALABLE: Persist the repeating response so the model sees its own mistake in history.
	if err := Turn.CtxManager.AddContent(ctx, Turn.State.Response); err != nil {
		return ProcessResult{}, err
	}

	// INTERCEPTABLE: Append the synthetic warning to history.
	// We use 'user' role as it's the most common way to provide feedback to the LLM.
	warning := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: LoopWarning}},
	}
	if err := Turn.CtxManager.AddContent(ctx, warning); err != nil {
		return ProcessResult{}, err
	}

	// RECOVERY: End the current Turn gracefully and signal the Run() loop to continue to a new generation Turn.
	Turn.State.Response = nil
	Turn.State.ToolResponse = nil
	Turn.State.HasToolCalls = true // Trick shouldStopRunning into starting a new Turn

	return ProcessResult{NextPhase: PhaseComplete}, nil
}

func isDuplicateResponse(currentHash string, recentHashes []string) bool {
	for _, prevHash := range recentHashes {
		if currentHash == prevHash {
			return true
		}
	}
	return false
}

// TruncateSafe truncates a byte slice to a specific number of runes safely.
// It uses a Go compiler optimization `range string(b)` to avoid heap allocation
// during rune decoding.
func TruncateSafe(b []byte, maxRunes int) string {
	count := 0
	// The Go compiler optimizes `range string(b)` to avoid heap allocation!
	for byteIndex := range string(b) {
		if count == maxRunes {
			return string(b[:byteIndex]) + "..." // Only allocates the small truncated snippet
		}
		count++
	}
	return string(b)
}
