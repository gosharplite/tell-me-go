// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

// PipelineFactory encapsulates the logic for creating context processing pipelines.
type PipelineFactory struct {
	Registry   *registry.Registry
	History    *history.Manager
	Summarizer HistorySummarizer
	Estimator  TokenEstimator
	Events     events.EventBus
}

// BuildStandardPipeline creates the default context transformation pipeline.
func (f *PipelineFactory) BuildStandardPipeline(limits events.Limits) *ContextPipeline {
	return NewContextPipeline(
		&HistoryPruner{
			Policy:  &SlidingWindowPolicy{MaxTurns: limits.MaxHistoryTurns},
			Manager: f.History,
		},
		&SystemInstructionInjector{
			Instructions: "You are an autonomous Software Development Agent. Follow the SOP: 1. Analyze 2. Plan 3. TDD 4. Standards 5. Review.",
		},
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
}
