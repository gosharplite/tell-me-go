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
