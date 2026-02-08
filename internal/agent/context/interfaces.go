// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// HistoryManager defines the interface for interacting with history.
type HistoryManager interface {
	GetContents() []*llm.Content
	SetContents(ctx context.Context, contents []*llm.Content) error
	Snapshot()
	Rollback(ctx context.Context)
	AddContent(ctx context.Context, content *llm.Content) error
	GetResolver() llm.AssetResolver
	SetPinned(ctx context.Context, turnIndex int, pinned bool) error
}

// ToolRegistry defines the interface for accessing tool declarations.
type ToolRegistry = tools.IToolRegistry

// TokenEstimator defines the interface for token counting.
type TokenEstimator interface {
	EstimateTokens(contents []*llm.Content) int
}

// PruningPolicy defines how to mark turns for pruning.
type PruningPolicy interface {
	MarkTurns(ctx context.Context, turns [][]*llm.Content, keep []bool) int
	Name() string
}

// ContextTransformer modifies the context before it's sent to the LLM.
type ContextTransformer interface {
	Transform(ctx context.Context, req *Request) error
	Priority() int // Lower runs first
}
