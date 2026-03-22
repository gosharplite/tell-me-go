// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// historyPruner enforces history turn limits using a policy.
type historyPruner struct {
	Policy ports.PruningPolicy
	Events events.EventBus
	Logger *slog.Logger
}

func (t *historyPruner) getLogger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

func (t *historyPruner) Transform(ctx context.Context, req *ports.ContextRequest) error {
	initialLen := len(req.History)
	if initialLen == 0 {
		return nil
	}

	// Group messages into turns (pairs)
	turns, err := groupTurns(ctx, req.History)
	if err != nil {
		return err
	}

	keep := make([]bool, len(turns))
	if req.Metadata.KeptByPolicy == nil {
		req.Metadata.KeptByPolicy = make(map[string]int)
	}

	if err := t.applyPruningPolicies(ctx, turns, keep, req.Metadata.KeptByPolicy); err != nil {
		return err
	}

	// Construct new history and count pruned turns
	newHistory, prunedCount, keptCount, err := t.reconstructHistory(ctx, turns, keep)
	if err != nil {
		return err
	}

	if prunedCount > 0 {
		req.History = newHistory
		req.Metadata.PrunedTurns += prunedCount
		req.Metadata.TotalTurnsKept += keptCount

		if t.Events != nil {
			evt := events.SystemMessageEvent{
				Message: fmt.Sprintf("History pruned: %d turns removed, %d turns remaining.", prunedCount, len(newHistory)/2),
				Level:   "info",
			}
			if err := events.SafePublish(ctx, t.Events, evt); err != nil {
				if !errors.Is(err, events.ErrBusNotInitialized) {
					t.getLogger().Error("event_publish_failed",
						slog.String("event_type", string(evt.Type())),
						slog.Any("error", err))
					return err
				}
			}
		}
	}

	return nil
}

func (t *historyPruner) applyPruningPolicies(ctx context.Context, turns [][]*llm.Content, keep []bool, keptByPolicy map[string]int) error {
	// If it's a composite policy, we track sub-policies individually.
	if cp, ok := t.Policy.(*compositePruningPolicy); ok {
		for _, p := range cp.Policies {
			count, err := p.MarkTurns(ctx, turns, keep)
			if err != nil {
				return err
			}
			keptByPolicy[p.Name()] = count
		}
	} else {
		count, err := t.Policy.MarkTurns(ctx, turns, keep)
		if err != nil {
			return err
		}
		keptByPolicy[t.Policy.Name()] = count
	}
	return nil
}

func (t *historyPruner) reconstructHistory(ctx context.Context, turns [][]*llm.Content, keep []bool) ([]*llm.Content, int, int, error) {
	var newHistory []*llm.Content
	prunedCount := 0
	keptCount := 0
	for i, k := range keep {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return nil, 0, 0, ctx.Err()
			default:
			}
		}

		if k {
			newHistory = append(newHistory, turns[i]...)
			keptCount++
		} else {
			prunedCount++
		}
	}
	return newHistory, prunedCount, keptCount, nil
}

func (t *historyPruner) Priority() int { return 110 }

// compositePruningPolicy aggregates multiple policies using OR logic.
type compositePruningPolicy struct {
	Policies []ports.PruningPolicy
}

func (p *compositePruningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	totalMarked := 0
	for _, policy := range p.Policies {
		count, err := policy.MarkTurns(ctx, turns, keep)
		if err != nil {
			return 0, err
		}
		totalMarked += count
	}
	return totalMarked, nil
}

func (p *compositePruningPolicy) Name() string { return "Composite" }

// slidingWindowPolicy keeps the last N turns.
type slidingWindowPolicy struct {
	MaxTurns int
}

func (p *slidingWindowPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	if p.MaxTurns <= 0 {
		return 0, nil
	}

	totalTurns := len(turns)
	startWindow := totalTurns - p.MaxTurns
	if startWindow < 0 {
		startWindow = 0
	}

	count := 0
	for i := startWindow; i < totalTurns; i++ {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}
		keep[i] = true
		count++
	}
	return count, nil
}

func (p *slidingWindowPolicy) Name() string { return "SlidingWindow" }

// importanceRankPolicy keeps turns containing function calls, responses, or data.
type importanceRankPolicy struct{}

func (p *importanceRankPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	count := 0
	for i, turn := range turns {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}

		if isTurnImportant(turn) {
			keep[i] = true
			count++
		}
	}
	return count, nil
}

func isTurnImportant(turn []*llm.Content) bool {
	for _, msg := range turn {
		if hasImportantParts(msg) {
			return true
		}
	}
	return false
}

func hasImportantParts(msg *llm.Content) bool {
	for _, part := range msg.Parts {
		if part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil {
			return true
		}
	}
	return false
}

func (p *importanceRankPolicy) Name() string { return "Importance" }

// pinningPolicy keeps turns that have at least one pinned message.
type pinningPolicy struct{}

func (p *pinningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error) {
	count := 0
	for i, turn := range turns {
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}

		pinned := false
		for _, msg := range turn {
			if msg.Pinned {
				pinned = true
				break
			}
		}

		if pinned {
			keep[i] = true
			count++
		}
	}
	return count, nil
}

func (p *pinningPolicy) Name() string { return "Pinning" }
