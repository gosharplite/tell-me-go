// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
)

// OptimizationProfile defines the behavior of the context pipeline.
type OptimizationProfile string

const (
	// ProfilePrecise prioritizes data integrity and code blocks.
	ProfilePrecise OptimizationProfile = "precise"
)

// PipelineFactory encapsulates the logic for creating context processing pipelines.
type PipelineFactory struct {
	Registry   tools.IToolRegistry
	History    *history.Manager
	Summarizer services.Summarizer
	Estimator  TokenEstimator
	Events     events.EventBus
	Profile    OptimizationProfile
}

// BuildStandardPipeline creates the default context transformation pipeline.
func (f *PipelineFactory) BuildStandardPipeline(limits events.Limits) *ContextPipeline {
	// 1. Calculate window size based on profile
	windowTurns := limits.MaxHistoryTurns
	if f.Profile == ProfilePrecise {
		// Precise mode: Shrink the sliding window to 50% (min 2)
		// to give ImportanceRankPolicy more token budget.
		windowTurns = limits.MaxHistoryTurns / 2
		if windowTurns < 2 {
			windowTurns = 2
		}
	}

	transformers := []ContextTransformer{
		&HistoryPruner{
			Policy: &CompositePruningPolicy{
				Policies: []PruningPolicy{
					// 2. Use the profile-adjusted window size
					&SlidingWindowPolicy{MaxTurns: windowTurns},
					&PinningPolicy{},
					&ImportanceRankPolicy{},
				},
			},
			Events: f.Events,
		},
	}

	transformers = append(transformers,
		&TokenGatekeeper{
			MaxTokens:  limits.MaxHistoryTokens,
			Estimator:  f.Estimator,
			Summarizer: f.Summarizer,
			Events:     f.Events,
		},
		&EmptyTurnFilter{},
		&WarningInjector{
			Strategy: f.Estimator.(*ContextStrategy),
		},
		&TransientMerger{},
		&FinalContextValidator{
			Strategy: f.Estimator.(*ContextStrategy),
		},
	)

	return NewContextPipeline(transformers...)
}
