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

	// GetResolver returns the asset resolver for resolving inline data
	// references (e.g., images, files) embedded in the conversation history.
	GetResolver() llm.AssetResolver
}

// HistoryWriter defines the interface for adding or modifying chat history.
type HistoryWriter interface {
	// SetContents replaces the entire in-memory history with the given
	// slice. The previous history is discarded without being persisted.
	// Call Save to persist the new contents to disk.
	SetContents(ctx context.Context, contents []*llm.Content) error

	// AddContent appends a single Content entry to the in-memory history.
	// The entry is not persisted until Save is called.
	AddContent(ctx context.Context, content *llm.Content) error

	// AppendParts appends one or more Parts to the Content at the given
	// index. This is used to attach multi-part responses (e.g., tool
	// results) to an existing message. The index must refer to an
	// existing Content entry.
	AppendParts(ctx context.Context, index int, parts []*llm.Part) error

	// Save persists the current in-memory history to stable storage.
	// It is a no-op if the history has not been modified since the last
	// save. Implementations must be safe for sequential use; concurrent
	// calls to Save and Sync are not required.
	Save(ctx context.Context) error

	// Sync flushes any buffered writes to stable storage without
	// performing a full save cycle. It is lighter-weight than Save
	// and suitable for use at the end of each turn.
	Sync(ctx context.Context) error
}

// HistoryModifier defines the interface for specialized history operations.
type HistoryModifier interface {
	// Archive moves the given contents from active memory to the
	// disk-based archive. Archived content is no longer accessible
	// via GetWindow but remains available through ArchiveReader.
	Archive(ctx context.Context, contents []*llm.Content) error

	// SetPinned marks or unmarks a specific turn as pinned. Pinned turns
	// are exempt from automatic pruning and summarization. turnIndex
	// refers to the turn number in active memory.
	SetPinned(ctx context.Context, turnIndex int, pinned bool) error

	// GetFilePath returns the filesystem path to the active history file.
	// This is primarily used for logging and diagnostic purposes.
	GetFilePath() string

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
