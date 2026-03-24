// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

const loopWarning = "SYSTEM WARNING: Infinite loop detected. You are repeating the exact same tool call or response. The previous action was aborted. Please analyze your previous steps and try a DIFFERENT tool or strategy."

// WithStreaming returns a middleware that injects a stream handler into the turn.
func (e *turnEngine) WithStreaming() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			if turn.State.Phase == phaseInference && e.events != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *llm.Content) {
					evt := events.ResponseStreamEvent{Context: ctx, Stream: stream}
					if err := events.SafePublish(ctx, e.events, evt); err != nil {
						if !errors.Is(err, events.ErrBusNotInitialized) {
							e.getLogger().Error("event_publish_failed",
								slog.String("event_type", string(evt.Type())),
								slog.Any("error", err))
						}
					}
				}
			}
			return next.process(ctx, turn)
		})
	}
}

// WithStatusReporter returns a middleware that publishes turn status events.
func (e *turnEngine) WithStatusReporter() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			res, err := next.process(ctx, turn)
			if e.events == nil || err != nil {
				return res, err
			}

			if (turn.State.Phase == phaseRefining && turn.State.RetryCount == 0) || turn.State.Phase == phasePersisting {
				limits := turn.CtxManager.GetLimits()
				maxTokens := limits.MaxHistoryTokens
				maxHistTurns := limits.MaxHistoryTurns
				threshold := limits.TieredThreshold

				var cost float64
				var dailyCost float64
				var totalM, totalH, totalO int64
				if turn.CostTracker != nil {
					cost = turn.CostTracker.GetTotalCost(ctx)
					dailyCost = turn.CostTracker.GetDailyCost(ctx)
					stats, _ := turn.CostTracker.GetStats(ctx)
					totalM = stats.PromptTokens - stats.CachedTokens
					totalH = stats.CachedTokens
					totalO = stats.ResponseTokens + stats.ThinkingTokens
				}

				evt := events.TurnStatusEvent{
					Status: events.TurnStatus{
						Timestamp:        turn.Clock.Now(),
						CurrentTurns:     turn.Index,
						SessionTurns:     turn.CtxManager.History.GetTotalEntries() / 2,
						MaxHistoryTurns:  maxHistTurns,
						Tokens:           turn.State.Tokens,
						MaxHistoryTokens: maxTokens,
						TieredThreshold:  threshold,
						Metrics:          turn.State.Metrics,
						IsPostCall:       turn.State.Phase == phasePersisting,
						StartTime:        turn.StartTime,
						SessionCost:      cost,
						DailyCost:        dailyCost,
						TaskCost:         turn.State.TaskCost,
						TotalM:           totalM,
						TotalH:           totalH,
						TotalO:           totalO,
					},
				}
				if err := events.SafePublish(ctx, e.events, evt); err != nil {
					if !errors.Is(err, events.ErrBusNotInitialized) {
						e.getLogger().Error("event_publish_failed",
							slog.String("event_type", string(evt.Type())),
							slog.Any("error", err))
					}
				}
			}
			return res, nil
		})
	}
}

// WithMetrics returns a middleware that publishes usage metrics.
func (e *turnEngine) WithMetrics() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			res, err := next.process(ctx, turn)
			if e.events != nil && turn.State.Phase == phasePersisting && turn.State.Metrics != nil {
				if turn.CostTracker != nil {
					// Calculate and accumulate into session total (thread-safe)
					turnCost := turn.CostTracker.AccumulateAndReturn(*turn.State.Metrics)

					// Populate fields for the UI/EventBus
					turn.State.Metrics.Cost = turnCost
					turn.State.TaskCost += turnCost
				}

				evt := events.UsageMetricsEvent{
					Context:   ctx,
					Metrics:   turn.State.Metrics,
					StartTime: turn.StartTime,
				}
				if err := events.SafePublish(ctx, e.events, evt); err != nil {
					if !errors.Is(err, events.ErrBusNotInitialized) {
						e.getLogger().Error("event_publish_failed",
							slog.String("event_type", string(evt.Type())),
							slog.Any("error", err))
					}
				}
			}
			return res, err
		})
	}
}

// withLoopDetector returns a middleware that detects and breaks infinite tool loops.
func withLoopDetector() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			res, err := next.process(ctx, turn)

			if turn.State.Phase == phaseInference && err == nil && turn.State.Response != nil {
				// 1. Multi-step loop detection (Text & Tool Calls)
				rawJSON, _ := json.Marshal(turn.State.Response)
				h := sha256.Sum256(rawJSON)
				currentHash := hex.EncodeToString(h[:])

				var loopDetected bool

				if isDuplicateResponse(currentHash, turn.State.RecentResponseHashes) {
					loopDetected = true
				} else {
					// Keep last N hashes (using the same repetition limit)
					turn.State.RecentResponseHashes = append(turn.State.RecentResponseHashes, currentHash)
					if len(turn.State.RecentResponseHashes) > domain_config.DefaultMaxLoopRepetitions {
						turn.State.RecentResponseHashes = turn.State.RecentResponseHashes[1:]
					}

					// 2. Tool call loop detection (Immediate threshold)
					for _, p := range turn.State.Response.Parts {
						if p.FunctionCall != nil {
							args, _ := json.Marshal(p.FunctionCall.Args)
							key := p.FunctionCall.Name + ":" + string(args)
							turn.State.ToolCallCount[key]++
							if turn.State.ToolCallCount[key] > domain_config.DefaultMaxLoopRepetitions {
								loopDetected = true
								break
							}
						}
					}
				}

				if loopDetected {
					// Publish warning event for UI visibility
					_ = events.SafePublish(ctx, turn.Events, events.SystemMessageEvent{
						Message: "Infinite loop detected! Injecting corrective feedback to break the cycle...",
						Level:   "warn",
					})

					// SCALABLE: Persist the repeating response so the model sees its own mistake in history.
					if err := turn.CtxManager.AddContent(ctx, turn.State.Response); err != nil {
						return processResult{}, err
					}

					// INTERCEPTABLE: Append the synthetic warning to history.
					// We use 'user' role as it's the most common way to provide feedback to the LLM.
					warning := &llm.Content{
						Role:  "user",
						Parts: []*llm.Part{{Text: loopWarning}},
					}
					if err := turn.CtxManager.AddContent(ctx, warning); err != nil {
						return processResult{}, err
					}

					// RECOVERY: End the current turn gracefully and signal the Run() loop to continue to a new generation turn.
					turn.State.Response = nil
					turn.State.ToolResponse = nil
					turn.State.HasToolCalls = true // Trick shouldStopRunning into starting a new turn

					return processResult{NextPhase: phaseComplete}, nil
				}
			}

			return res, err
		})
	}
}

func isDuplicateResponse(currentHash string, recentHashes []string) bool {
	for _, prevHash := range recentHashes {
		if currentHash == prevHash {
			return true
		}
	}
	return false
}

// truncateSafe truncates a byte slice to a specific number of runes safely.
// It uses a Go compiler optimization `range string(b)` to avoid heap allocation
// during rune decoding.
func truncateSafe(b []byte, maxRunes int) string {
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
