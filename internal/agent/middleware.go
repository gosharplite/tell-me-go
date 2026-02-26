// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// WithStreaming returns a middleware that injects a stream handler into the turn.
func (e *turnEngine) WithStreaming() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) (processResult, error) {
			if turn.State.Phase == phaseInference && e.events != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *llm.Content) {
					e.events.Publish(events.ResponseStreamEvent{Context: ctx, Stream: stream})
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

				e.events.Publish(events.TurnStatusEvent{
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
				})
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

				e.events.Publish(events.UsageMetricsEvent{
					Context:   ctx,
					Metrics:   turn.State.Metrics,
					StartTime: turn.StartTime,
				})
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

				if err := checkDuplicateResponse(currentHash, turn.State.RecentResponseHashes, rawJSON); err != nil {
					return processResult{Stop: true}, err
				}
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
							return processResult{Stop: true}, newAgentError(errLogic, fmt.Sprintf("infinite loop detected: tool '%s' called with same arguments %d times", p.FunctionCall.Name, turn.State.ToolCallCount[key]), nil)
						}
					}
				}
			}

			return res, err
		})
	}
}

func checkDuplicateResponse(currentHash string, recentHashes []string, rawJSON []byte) error {
	for _, prevHash := range recentHashes {
		if currentHash == prevHash {
			snippet := string(rawJSON)
			if len(snippet) > 150 {
				snippet = snippet[:147] + "..."
			}
			errMsg := fmt.Sprintf("infinite loop detected: model is repeating a previous response: %s", snippet)
			return newAgentError(errLogic, errMsg, nil)
		}
	}
	return nil
}
