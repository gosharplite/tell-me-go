// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"errors"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// Sentinel errors for history operations.
var (
	ErrHistoryNotFound = errors.New("history not found")
)

// HistoryReader defines the interface for reading chat history.
type HistoryReader interface {
	// GetWindow returns a deep copy of a specific range of history.
	// If endIdx is -1, it returns from startIdx to the end of the history.
	GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error)

	// GetTotalEntries returns the total number of content entries currently stored.
	GetTotalEntries() int

	// GetLastUserMessage finds the text of the last user message and the number of turns to rollback.
	GetLastUserMessage(ctx context.Context) (msg string, turnsToRollback int, err error)

	GetResolver() llm.AssetResolver
}

// HistoryWriter defines the interface for adding or modifying chat history.
type HistoryWriter interface {
	SetContents(ctx context.Context, contents []*llm.Content) error
	AddContent(ctx context.Context, content *llm.Content) error
	AppendParts(ctx context.Context, index int, parts []*llm.Part) error
	Save(ctx context.Context) error
}

// HistoryModifier defines the interface for specialized history operations.
type HistoryModifier interface {
	Archive(ctx context.Context, contents []*llm.Content) error
	SetPinned(ctx context.Context, turnIndex int, pinned bool) error

	// RollbackTurns removes the last N turns (1 turn = 2 messages) from the history.
	// It returns the actual number of turns removed, the remaining turns, the remaining total messages, and any error.
	RollbackTurns(ctx context.Context, turns int) (actualRemoved int, remainingTurns int, remainingMsgs int, err error)
}

// HistoryManager defines the interface for interacting with history.
type HistoryManager interface {
	HistoryReader
	HistoryWriter
	HistoryModifier
}
