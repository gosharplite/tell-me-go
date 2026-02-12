// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// optimizationProfile defines the behavior of the context pipeline.
type optimizationProfile string

const (
	// profilePrecise prioritizes data integrity and code blocks.
	profilePrecise optimizationProfile = "precise"
)

// PipelineFactory encapsulates the logic for creating context processing pipelines.
type PipelineFactory struct {
	Registry   tools.IToolRegistry
	History    services.HistoryManager
	Summarizer services.Summarizer
	Estimator  llm.TokenEstimator
	Events     events.EventBus
	Profile    optimizationProfile
}

// BuildStandardPipeline creates the default context transformation pipeline.
func (f *PipelineFactory) BuildStandardPipeline(limits events.Limits) *ContextPipeline {
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

	transformers := []services.ContextTransformer{
		&historyRepairer{},
		&contentCleaner{},
		&historyPruner{
			Policy: &compositePruningPolicy{
				Policies: []services.PruningPolicy{
					// 2. Use the profile-adjusted window size
					&slidingWindowPolicy{MaxTurns: windowTurns},
					&pinningPolicy{},
					&importanceRankPolicy{},
				},
			},
			Events: f.Events,
		},
	}

	transformers = append(transformers,
		&tokenGatekeeper{
			MaxTokens:  limits.MaxHistoryTokens,
			Estimator:  f.Estimator,
			Summarizer: f.Summarizer,
			Events:     f.Events,
		},
		&emptyTurnFilter{},
		&warningInjector{
			Strategy: f.Estimator.(*ContextStrategy),
		},
		&transientMerger{},
		&finalContextValidator{
			Strategy: f.Estimator.(*ContextStrategy),
		},
	)

	return NewContextPipeline(transformers...)
}
