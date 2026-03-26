package ports

import (
	"context"
	"time"
)

// HistoryViewDTO is the immutable read-model for the UI.
// NEVER pass llm.Content directly to the TUI to prevent mutation and rendering crashes.
type HistoryViewDTO struct {
	ID             string
	Role           string
	ContentPreview string // Masked/formatted text (e.g., base64 attachments hidden)
	ThoughtProcess string // Extracted reasoning blocks
	IsArchived     bool   // True if sourced from the disk archive (disables UI mutation actions)
	IsPinned       bool   // True if the turn is pinned in active memory
	OriginalIndex  int    // The underlying index in active memory (used for mutation)
	Timestamp      time.Time
	ToolCalls      []string // Summarized tool execution names
}

// ArchiveReader provides O(1) unbounded reading of disk history.
type ArchiveReader interface {
	// ReadPage uses byte-offsets to read archived history without full memory loading.
	// Returns the parsed DTOs, the next byte offset cursor, and any error.
	ReadPage(ctx context.Context, limit int, offset int64) ([]HistoryViewDTO, int64, error)
}

// UnifiedHistoryProvider stitches active memory and archived history for the TUI.
type UnifiedHistoryProvider interface {
	// GetHistoryStream returns a unified, read-only stream of history.
	GetHistoryStream(ctx context.Context, limit int, cursor string) ([]HistoryViewDTO, string, error)
}
