// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// HistoryManagerProvider defines the interface for providing history-related services.
// NOTE: This interface is for infrastructure use and should be resolved by the CLI/Wiring layer.
type HistoryManagerProvider interface {
	// GetHistoryManager loads the history manager for a given configuration.
	GetHistoryManager(ctx context.Context, cfg *config.Config) (HistoryManager, error)

	// GetUnifiedHistoryProvider assembles the read-model for the history browser.
	GetUnifiedHistoryProvider(ctx context.Context, cfg *config.Config, hManager HistoryManager) (UnifiedHistoryProvider, error)
}

// SuggestionProvider defines the interface for providing suggestion services.
// NOTE: This interface is for infrastructure use and should be resolved by the CLI/Wiring layer.
type SuggestionProvider interface {
	// GetSuggestionService initializes and returns the suggestion service.
	GetSuggestionService(ctx context.Context, recentHistory []string) (SuggestionService, error)
}
