// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// historyPruner enforces history turn limits using a policy.
type historyPruner struct {
	Policy services.PruningPolicy
	Events events.EventBus
}

func (t *historyPruner) Transform(ctx context.Context, req *services.ContextRequest) error {
	initialLen := len(req.History)
	if initialLen == 0 {
		return nil
	}

	// Group messages into turns (pairs)
	turns := groupTurns(req.History)

	keep := make([]bool, len(turns))
	if req.Metadata.KeptByPolicy == nil {
		req.Metadata.KeptByPolicy = make(map[string]int)
	}

	// If it's a composite policy, we track sub-policies individually.
	if cp, ok := t.Policy.(*CompositePruningPolicy); ok {
		for _, p := range cp.Policies {
			req.Metadata.KeptByPolicy[p.Name()] = p.MarkTurns(ctx, turns, keep)
		}
	} else {
		req.Metadata.KeptByPolicy[t.Policy.Name()] = t.Policy.MarkTurns(ctx, turns, keep)
	}

	// Construct new history and count pruned turns
	var newHistory []*llm.Content
	prunedCount := 0
	for i, k := range keep {
		if k {
			newHistory = append(newHistory, turns[i]...)
			req.Metadata.TotalTurnsKept++
		} else {
			prunedCount++
		}
	}

	if prunedCount > 0 {
		req.History = newHistory
		req.Metadata.PrunedTurns += prunedCount
		req.PersistHistory = true

		if t.Events != nil {
			t.Events.Publish(events.SystemMessageEvent{
				Message: fmt.Sprintf("History pruned: %d turns removed, %d turns remaining.", prunedCount, len(newHistory)/2),
				Level:   "info",
			})
		}
	}

	return nil
}

func (t *historyPruner) Priority() int { return 1 }

// CompositePruningPolicy aggregates multiple policies using OR logic.
type CompositePruningPolicy struct {
	Policies []services.PruningPolicy
}

func (p *CompositePruningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	totalMarked := 0
	for _, policy := range p.Policies {
		totalMarked += policy.MarkTurns(ctx, turns, keep)
	}
	return totalMarked
}

func (p *CompositePruningPolicy) Name() string { return "Composite" }

// SlidingWindowPolicy keeps the last N turns.
type SlidingWindowPolicy struct {
	MaxTurns int
}

func (p *SlidingWindowPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	if p.MaxTurns <= 0 {
		return 0
	}

	totalTurns := len(turns)
	startWindow := totalTurns - p.MaxTurns
	if startWindow < 0 {
		startWindow = 0
	}

	count := 0
	for i := startWindow; i < totalTurns; i++ {
		keep[i] = true
		count++
	}
	return count
}

func (p *SlidingWindowPolicy) Name() string { return "SlidingWindow" }

// ImportanceRankPolicy keeps turns containing function calls, responses, or data.
type ImportanceRankPolicy struct{}

func (p *ImportanceRankPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	count := 0
	for i, turn := range turns {
		important := false
		for _, msg := range turn {
			for _, part := range msg.Parts {
				if part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil {
					important = true
					break
				}
			}
			if important {
				break
			}
		}

		if important {
			keep[i] = true
			count++
		}
	}
	return count
}

func (p *ImportanceRankPolicy) Name() string { return "Importance" }

// PinningPolicy keeps turns that have at least one pinned message.
type PinningPolicy struct{}

func (p *PinningPolicy) MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int {
	count := 0
	for i, turn := range turns {
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
	return count
}

func (p *PinningPolicy) Name() string { return "Pinning" }
