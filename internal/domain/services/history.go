// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// HistoryManager defines the interface for interacting with history.
type HistoryManager interface {
	GetContents() []*llm.Content
	SetContents(ctx context.Context, contents []*llm.Content) error
	Archive(ctx context.Context, contents []*llm.Content) error
	AppendParts(ctx context.Context, index int, parts []*llm.Part) error
	AddContent(ctx context.Context, content *llm.Content) error
	GetResolver() llm.AssetResolver
	SetPinned(ctx context.Context, turnIndex int, pinned bool) error
	Save(ctx context.Context) error
}
