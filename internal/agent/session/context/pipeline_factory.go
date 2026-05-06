// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// optimizationProfile defines the behavior of the context pipeline.
type optimizationProfile string

const (
	// profilePrecise prioritizes data integrity and code blocks.
	profilePrecise optimizationProfile = "precise"
)

// Factory encapsulates the logic for creating context processing pipelines.
type Factory struct {
	Registry   tools.Registry
	History    ports.HistoryManager
	Summarizer ports.Summarizer
	Estimator  llm.TokenEstimator
	Events     events.EventBus
	Profile    optimizationProfile
	Logger     ports.Logger
	Extras     []ports.ContextTransformer // Injected by session/ layer (e.g., skillInjector)
}

// BuildStandardPipeline creates the default context transformation pipeline.
// extras allows the parent session/ package to inject additional transformers
// (e.g., skillInjector) without creating a context → domain/skills import.
func (f *Factory) BuildStandardPipeline(limits events.Limits, extras ...ports.ContextTransformer) *contextPipeline {
	// 1. Calculate window size based on profile
	windowTurns := limits.MaxHistoryTurns
	if f.Profile == profilePrecise {
		// Precise mode: Shrink the sliding window to 50% (min 2)
		// to give importanceRankPolicy more token budget.
		windowTurns = limits.MaxHistoryTurns / 2
		if windowTurns < 2 {
			windowTurns = 2
		}
	}

	transformers := []ports.ContextTransformer{
		&HistoryRepairer{},
	}
	transformers = append(transformers, extras...)
	transformers = append(transformers,
		&toolResponseCleaner{}, // Remove tool responses with empty IDs
		&emptyMessagePruner{},  // Explicitly drop messages with 0 parts
		&contentCleaner{},
		&thoughtSignaturePropagator{},
	)

	// Only add the HistoryPruner if turn limits are actually configured.
	// If limits.MaxHistoryTurns <= 0, turn-based pruning is disabled entirely.
	if limits.MaxHistoryTurns > 0 {
		transformers = append(transformers, &HistoryPruner{
			Policy: &compositePruningPolicy{
				Policies: []ports.PruningPolicy{
					// 2. Use the profile-adjusted window size
					&SlidingWindowPolicy{MaxTurns: windowTurns},
					&pinningPolicy{},
					&importanceRankPolicy{},
				},
			},
			Events: f.Events,
			Logger: f.Logger,
		})
	}

	transformers = append(transformers,
		newTokenGatekeeper(
			f.Estimator.(TokenEstimator),
			f.Summarizer,
			withMaxTokens(limits.MaxHistoryTokens),
			withEvents(f.Events),
			withLogger(f.Logger),
		),
		&emptyTurnFilter{},
		&WarningInjector{
			Strategy: f.Estimator.(*Strategy),
		},
		&TransientMerger{},
		&finalContextValidator{
			Strategy: f.Estimator.(*Strategy),
		},
	)

	return NewContextPipeline(transformers...)
}
