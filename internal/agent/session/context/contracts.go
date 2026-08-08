// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// ContextMetadata contains diagnostics and auxiliary data produced by the
// context preparation pipeline (pruning, summarization, truncation).
//
// Consumers receive this via ContextRequest and should treat it as
// read-only except for Warnings (which may be appended to).
type ContextMetadata struct {
	// OriginalTokenCount is the token count of the full history before any
	// pruning or summarization was applied.
	OriginalTokenCount int
	// FinalTokenCount is the token count after all transformations.
	FinalTokenCount int
	// FinalTurnCount is the number of conversation turns retained after pruning.
	FinalTurnCount int
	// PrunedTurns is the number of turns removed by pruning policies.
	PrunedTurns int
	// SummarizedTurns is the number of turns replaced by a summary.
	SummarizedTurns int
	// SummarizationAttempted is true if the summarizer was invoked,
	// regardless of whether it succeeded.
	SummarizationAttempted bool
	// MaintenanceBlocked is true if context maintenance was skipped
	// (e.g., because the conversation is too short to benefit).
	MaintenanceBlocked bool
	// Warnings collects non-fatal diagnostic messages from the pipeline.
	// Consumers may append to this slice.
	Warnings []string
	// TotalTurnsKept is the total number of turns retained across all
	// retention policies.
	TotalTurnsKept int
	// KeptByPolicy maps each pruning policy name to the number of turns
	// that policy elected to keep.
	KeptByPolicy map[string]int
	// History is the post-transform content slice that will be sent to
	// the LLM. Callers must Clone before mutating.
	History []*llm.Content
}

// ContextRequest represents the input and state of a context preparation pipeline.
type ContextRequest struct {
	// Turn is the zero-based index of the current conversation turn.
	Turn int
	// History is the raw, pre-transform conversation history.
	// Transformers may replace or truncate this slice.
	History []*llm.Content
	// Metadata accumulates diagnostics as the request flows through the
	// transformer chain. Transformers should update relevant fields.
	Metadata ContextMetadata
	// PersistHistory indicates whether the final History should be
	// persisted after the pipeline completes. Set by the orchestrator.
	PersistHistory bool
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	// MarkTurns evaluates each turn group and sets the corresponding
	// keep[i] to true if the turn should be retained. It returns the
	// number of turns marked for retention. A non-nil error aborts the
	// pruning pipeline.
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) (int, error)

	// Name returns a human-readable identifier for this policy,
	// used in ContextMetadata.KeptByPolicy and diagnostic logs.
	Name() string
}

// ContextTransformer modifies the context before it's sent to the LLM.
// Transformers are applied in ascending Priority order.
type ContextTransformer interface {
	// Transform applies a mutation to the ContextRequest. Common
	// transformations include summarization, truncation, and pruning.
	// Implementations must be safe for single-goroutine sequential use;
	// concurrent invocation is not required.
	Transform(ctx context.Context, req *ContextRequest) error

	// Priority returns the execution order of this transformer.
	// Lower values execute first. Values need not be contiguous.
	Priority() int
}

// clone creates a deep copy of the ContextMetadata.
func (m *ContextMetadata) clone() *ContextMetadata {
	cloned := *m
	if m.Warnings != nil {
		cloned.Warnings = make([]string, len(m.Warnings))
		copy(cloned.Warnings, m.Warnings)
	}
	if m.KeptByPolicy != nil {
		cloned.KeptByPolicy = make(map[string]int)
		for k, v := range m.KeptByPolicy {
			cloned.KeptByPolicy[k] = v
		}
	}
	if m.History != nil {
		cloned.History = make([]*llm.Content, len(m.History))
		for i, c := range m.History {
			cloned.History[i] = llm.CloneContent(c)
		}
	}
	return &cloned
}
