// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
)

// HistoryBrowser defines the interface for launching a history browser UI.
type HistoryBrowser interface {
	// Browse launches an interactive browser for viewing history.
	// It blocks until the browser is closed.
	Browse(ctx context.Context, provider UnifiedHistoryProvider, hManager HistoryManager) error
}

// HistoryEditor defines the interface for launching an interactive turn editor.
type HistoryEditor interface {
	// Edit launches an interactive TUI that lets the user modify the last
	// model turn's text and thought content. It blocks until the editor is
	// dismissed. Returns nil on successful save, or an error if the user
	// aborted (context.Canceled) or a system error occurred.
	Edit(ctx context.Context, hManager HistoryManager) error
}
