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
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
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

	var cost float64
	var totalM, totalH, totalO int64
	if Turn.CostTracker != nil {
		var stats domain_pricing.UsageStats
		stats, cost = Turn.CostTracker.GetStats(ctx)
		totalM = stats.PromptTokens - stats.CachedTokens
		totalH = stats.CachedTokens
		totalO = stats.ResponseTokens + stats.ThinkingTokens
	}

	evt := events.TurnStatusEvent{
		Status: events.TurnStatus{
			Timestamp:        Turn.Clock.Now(),
			CurrentTurns:     Turn.Index,
			SessionTurns:     Turn.SessionTurnsAtStart,
			MaxHistoryTurns:  maxHistTurns,
			Tokens:           Turn.State.Tokens,
			MaxHistoryTokens: maxTokens,
			Metrics:          Turn.State.Metrics,
			IsPostCall:       isPostCall,
			IsFinal:          isFinal,
			StartTime:        Turn.StartTime,
			SessionCost:      cost,
			TaskCost:         Turn.State.TaskCost,
			TotalM:           totalM,
			TotalH:           totalH,
			TotalO:           totalO,
			ToolReasons:      Turn.State.ToolReasons,
			Mode:             Turn.Mode,
			Model:            Turn.Model,
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

// loopDetector owns the Run-scoped accumulators for hallucination-loop
// detection: the tool-call repetition counter and the sliding window of
// recent response hashes. Constructed once per Engine, reset once per Run.
// Single-Run-at-a-time contract (one agent Chat at a time); intentionally
// lock-free, matching the pre-T6 TurnState design.
type loopDetector struct {
	toolCallCount        map[string]int
	recentResponseHashes []string
	seenRateLimit        bool
}

func newLoopDetector() *loopDetector {
	return &loopDetector{toolCallCount: make(map[string]int)}
}

// reset clears all accumulators for a fresh Run. MUST re-allocate the map —
// map writes on a nil map panic (append on a nil slice is fine).
func (d *loopDetector) reset() {
	d.toolCallCount = make(map[string]int)
	d.recentResponseHashes = nil
	d.seenRateLimit = false
}

// hasSeenRateLimit reports whether a rate-limit error has occurred during
// this Run. Nil-safe: a nil detector (bare-Turn unit tests/benches) reports
// false, mirroring the ADR-059 nil-safe read pattern.
func (d *loopDetector) hasSeenRateLimit() bool { return d != nil && d.seenRateLimit }

// recordRateLimit marks that a rate-limit error occurred. Nil-safe no-op.
func (d *loopDetector) recordRateLimit() { if d != nil { d.seenRateLimit = true } }

// withLoopDetector returns a middleware that detects and breaks infinite tool loops.
func (e *Engine) withLoopDetector() turnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, Turn *Turn) (ProcessResult, error) {
			res, err := next.Process(ctx, Turn)
			if Turn.State.Phase != PhaseInference || err != nil || Turn.State.Response == nil {
				return res, err
			}

			if e.loopDetector != nil && e.loopDetector.detectLoop(Turn.State) {
				return handleLoopBreak(ctx, Turn)
			}

			return res, err
		})
	}
}

func (d *loopDetector) detectLoop(state *TurnState) bool {
	// 1. Multi-step loop detection (Text & Tool Calls)
	// Exclude mutable fields (ID) from the hash to ensure consistent
	// comparison across turns. AddContent mutates content.ID in-place,
	// so hashing the raw response would produce different hashes for
	// structurally identical responses on different turns.
	sanitized := *state.Response
	sanitized.ID = ""
	rawJSON, err := json.Marshal(&sanitized)
	if err != nil {
		// Architect-acceptance (2026-07): json.Marshal on *llm.Content cannot fail —
		// all fields are simple types (string, []*Part, etc.). Same acceptance class
		// as json.Marshal on all-string structs in global_prompt_tracker.go.
		return false
	}
	h := sha256.Sum256(rawJSON)
	currentHash := hex.EncodeToString(h[:])

	if d.isDuplicateResponse(currentHash) {
		return true
	}

	// Keep last N hashes (using the same repetition limit)
	d.recentResponseHashes = append(d.recentResponseHashes, currentHash)
	if len(d.recentResponseHashes) > domain_config.DefaultMaxLoopRepetitions {
		d.recentResponseHashes = d.recentResponseHashes[1:]
	}

	// 2. Tool call loop detection (Immediate threshold)
	for _, p := range state.Response.Parts {
		if p.FunctionCall != nil {
			args, _ := json.Marshal(p.FunctionCall.Args)
			key := p.FunctionCall.Name + ":" + string(args)
			d.toolCallCount[key]++
			if d.toolCallCount[key] > domain_config.DefaultMaxLoopRepetitions {
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

	// Persist the repeating response so the model sees its own mistake in history.
	if err := Turn.CtxManager.AddContent(ctx, Turn.State.Response); err != nil {
		return ProcessResult{}, err
	}

	if err := injectSyntheticLoopFeedback(ctx, Turn); err != nil {
		return ProcessResult{}, err
	}

	// RECOVERY: End the current Turn gracefully and signal the Run() loop
	// to continue to a new generation Turn.
	Turn.State.Response = nil
	Turn.State.ToolResponse = nil
	Turn.State.HasToolCalls = true

	return ProcessResult{NextPhase: PhaseComplete}, nil
}

// injectSyntheticLoopFeedback appends corrective feedback to history
// that satisfies the LLM API contract after a loop is detected.
//
// For tool-call loops: synthetic "tool"-role FunctionResponse entries
// are paired with each FunctionCall, satisfying the tool_calls→tool_results
// adjacency requirement while preserving the model's chain-of-thought text.
//
// For text-only loops: a "user"-role warning is appended as before.
func injectSyntheticLoopFeedback(ctx context.Context, Turn *Turn) error {
	if Turn.State.HasToolCalls && Turn.State.Response != nil {
		// Collect all synthetic FunctionResponse parts first,
		// then inject as a single "tool"-role message. This
		// satisfies strict LLM role alternation rules and avoids
		// relying on AddContent's same-role merge side-effect.
		var syntheticParts []*llm.Part
		for _, part := range Turn.State.Response.Parts {
			if part.FunctionCall != nil {
				syntheticParts = append(syntheticParts, &llm.Part{
					FunctionResponse: &llm.FunctionResponse{
						ID:       part.FunctionCall.ID,
						Name:     part.FunctionCall.Name,
						Response: map[string]interface{}{"error": LoopWarning},
					},
				})
			}
		}
		if len(syntheticParts) > 0 {
			return Turn.CtxManager.AddContent(ctx, &llm.Content{
				Role:  "tool",
				Parts: syntheticParts,
			})
		}
		// Architect-acceptance (2026-07): reached only when HasToolCalls is true
		// but Response has no FunctionCall parts — inconsistent internal state that
		// does not occur in normal operation (HasToolCalls is derived from the
		// presence of FunctionCall parts). Defensive guard — same acceptance class
		// as defensive nil/empty guards on internal pipeline state (2026-07 Batch
		// Triage). See: docs/architect/INTENTIONAL_NON_FIXES.md.
		return nil
	}

	warning := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: LoopWarning}},
	}
	return Turn.CtxManager.AddContent(ctx, warning)
}

func (d *loopDetector) isDuplicateResponse(currentHash string) bool {
	for _, prevHash := range d.recentResponseHashes {
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
