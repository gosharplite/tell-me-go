// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ContextMetadata contains diagnostics and auxiliary data from the pipeline.
type ContextMetadata struct {
	OriginalTokenCount     int
	FinalTokenCount        int
	FinalTurnCount         int
	PrunedTurns            int
	SummarizedTurns        int
	SummarizationAttempted bool
	MaintenanceBlocked     bool
	Warnings               []string
	TotalTurnsKept         int
	KeptByPolicy           map[string]int
	History                []*llm.Content
}

// ContextRequest represents the input and state of a context preparation pipeline.
type ContextRequest struct {
	Turn           int
	History        []*llm.Content
	Metadata       ContextMetadata
	PersistHistory bool
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int
	Name() string
}

// ContextTransformer modifies the context before it's sent to the LLM.
type ContextTransformer interface {
	Transform(ctx context.Context, req *ContextRequest) error
	Priority() int
}

// ResultStrategy defines how tool outputs are transformed back into LLM messages.
type ResultStrategy interface {
	Format(call *llm.FunctionCall, result tools.ToolResult) *llm.Part
}
