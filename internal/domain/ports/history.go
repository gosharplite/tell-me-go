// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// HistoryManager defines the interface for interacting with history.
type HistoryManager interface {
	// GetWindow returns a deep copy of a specific range of history.
	// If endIdx is -1, it returns from startIdx to the end of the history.
	GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error)

	// GetTotalEntries returns the total number of content entries currently stored.
	GetTotalEntries() int

	SetContents(ctx context.Context, contents []*llm.Content) error
	Archive(ctx context.Context, contents []*llm.Content) error
	AppendParts(ctx context.Context, index int, parts []*llm.Part) error
	AddContent(ctx context.Context, content *llm.Content) error
	GetResolver() llm.AssetResolver
	SetPinned(ctx context.Context, turnIndex int, pinned bool) error
	Save(ctx context.Context) error
}
