// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

// OptimizationProfile defines the behavior of the context pipeline.
type OptimizationProfile string

const (
	// ProfilePrecise prioritizes data integrity and code blocks.
	ProfilePrecise OptimizationProfile = "precise"
	// ProfileChatty prioritizes dialogue and recent turns.
	ProfileChatty OptimizationProfile = "chatty"
)

// PipelineFactory encapsulates the logic for creating context processing pipelines.
type PipelineFactory struct {
	Registry   *registry.Registry
	History    *history.Manager
	Summarizer HistorySummarizer
	Estimator  TokenEstimator
	Events     events.EventBus
	Profile    OptimizationProfile
}

// BuildStandardPipeline creates the default context transformation pipeline.
func (f *PipelineFactory) BuildStandardPipeline(limits events.Limits) *ContextPipeline {
	transformers := []ContextTransformer{
		&HistoryPruner{
			Policy:  &SlidingWindowPolicy{MaxTurns: limits.MaxHistoryTurns},
			Manager: f.History,
		},
	}

	transformers = append(transformers,
		&TokenGatekeeper{
			MaxTokens:  limits.MaxHistoryTokens,
			Estimator:  f.Estimator,
			Summarizer: f.Summarizer,
			Manager:    f.History,
			Events:     f.Events,
		},
		&ToolDeclarationGenerator{
			Registry: f.Registry,
		},
		&WarningInjector{
			Strategy: f.Estimator.(*ContextStrategy),
		},
	)

	return NewContextPipeline(transformers...)
}
