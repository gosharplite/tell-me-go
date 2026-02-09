// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// WithStreaming returns a middleware that injects a stream handler into the turn.
func (e *TurnEngine) WithStreaming() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) processResult {
			if turn.State.Phase == phaseInference && e.events != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *llm.Content) {
					e.events.Publish(events.ResponseStreamEvent{Context: ctx, Stream: stream})
				}
			}
			return next.Process(ctx, turn)
		})
	}
}

// WithStatusReporter returns a middleware that publishes turn status events.
func (e *TurnEngine) WithStatusReporter() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) processResult {
			res := next.Process(ctx, turn)
			if e.events == nil || res.Error != nil {
				return res
			}

			if turn.State.Phase == phaseRefining || turn.State.Phase == phasePersisting {
				maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
				threshold := turn.CtxManager.Strategy.GetTieredThreshold()

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
						SessionTurns:     len(turn.CtxManager.History.GetContents()) / 2,
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
			return res
		})
	}
}

// WithMetrics returns a middleware that publishes usage metrics.
func (e *TurnEngine) WithMetrics() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) processResult {
			res := next.Process(ctx, turn)
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
			return res
		})
	}
}

// WithLoopDetector returns a middleware that detects and breaks infinite tool loops.
func WithLoopDetector() turnMiddleware {
	return func(next turnProcessor) turnProcessor {
		return turnProcessorFunc(func(ctx context.Context, turn *turn) processResult {
			res := next.Process(ctx, turn)

			if turn.State.Phase == phaseInference && res.Error == nil && turn.State.Response != nil {
				// 1. Multi-step loop detection (Text & Tool Calls)
				rawJSON, _ := json.Marshal(turn.State.Response)
				h := sha256.Sum256(rawJSON)
				currentHash := hex.EncodeToString(h[:])

				for _, prevHash := range turn.State.RecentResponseHashes {
					if currentHash == prevHash {
						return processResult{
							Stop:  true,
							Error: NewAgentError(ErrLogic, "infinite loop detected: model is repeating a previous response (content or tool calls)", nil),
						}
					}
				}
				// Keep last N hashes (using the same repetition limit)
				turn.State.RecentResponseHashes = append(turn.State.RecentResponseHashes, currentHash)
				if len(turn.State.RecentResponseHashes) > config.DefaultMaxLoopRepetitions {
					turn.State.RecentResponseHashes = turn.State.RecentResponseHashes[1:]
				}

				// 2. Tool call loop detection (Immediate threshold)
				for _, p := range turn.State.Response.Parts {
					if p.FunctionCall != nil {
						args, _ := json.Marshal(p.FunctionCall.Args)
						key := p.FunctionCall.Name + ":" + string(args)
						turn.State.ToolCallCount[key]++
						if turn.State.ToolCallCount[key] > config.DefaultMaxLoopRepetitions {
							return processResult{
								Stop:  true,
								Error: NewAgentError(ErrLogic, fmt.Sprintf("infinite loop detected: tool '%s' called with same arguments %d times", p.FunctionCall.Name, turn.State.ToolCallCount[key]), nil),
							}
						}
					}
				}
			}

			return res
		})
	}
}
