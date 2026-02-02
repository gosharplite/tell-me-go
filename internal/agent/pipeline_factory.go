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
	profile := f.Profile
	if profile == "" {
		profile = ProfilePrecise
	}

	transformers := []ContextTransformer{
		&HistoryPruner{
			Policy:  &SlidingWindowPolicy{MaxTurns: limits.MaxHistoryTurns},
			Manager: f.History,
		},
	}

	// Dynamic logic based on profile
	if profile == ProfilePrecise {
		transformers = append(transformers, &SystemInstructionInjector{
			Instructions: "You are an autonomous Software Development Agent. Follow the SOP: 1. Analyze 2. Plan 3. TDD 4. Standards 5. Review. Be precise and concise. Note: Only the Coder role has WRITE access to the filesystem.",
		})
	} else {
		transformers = append(transformers, &SystemInstructionInjector{
			Instructions: "You are a helpful AI assistant. Be chatty and friendly.",
		})
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
