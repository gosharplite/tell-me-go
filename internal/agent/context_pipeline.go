// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/types"
)

// ContextMetadata provides observability into how the context was processed.
type ContextMetadata struct {
	OriginalTokenCount int
	FinalTokenCount    int
	FinalTurnCount     int
	PrunedTurns        int
	SummarizedTurns    int
	Warnings           []string
}

// ContextRequest carries state through the context transformation pipeline.
type ContextRequest struct {
	Turn     int
	History  []*types.Content
	Result   []*types.Content
	Metadata ContextMetadata
}

// ContextTransformer defines a stage in the context preparation pipeline.
type ContextTransformer interface {
	Transform(ctx context.Context, req *ContextRequest) error
	Priority() int
}

// TokenEstimator decouples the manager from specific counting logic.
type TokenEstimator interface {
	EstimateTokens(contents []*types.Content) int
}

// PruningPolicy defines a strategy for reducing history size.
type PruningPolicy interface {
	Prune(ctx context.Context, history []*types.Content) ([]*types.Content, int)
}

// HistorySummarizer defines the interface for the summarization service.
type HistorySummarizer interface {
	Summarize(ctx context.Context, subset []*types.Content, focus string) (string, error)
}
