// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	stdctx "context"
	"io"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/application/suggestions"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// BuildSuggestionService constructs the suggestion service with the global
// prompt tracker, degrading to a no-op tracker when initialization fails.
func BuildSuggestionService(ctx stdctx.Context, fs infra_persistence.FileSystem, homeDir string, stderr io.Writer, logger *slog.Logger, wp services.WorkspacePolicy, recentHistory []string) (ports.SuggestionService, error) {
	tracker, err := history.NewGlobalPromptTracker(infra_persistence.NewDomainFS(fs), homeDir)
	if err != nil {
		logger.Warn("failed to initialize global prompt tracker, falling back to no-op", "error", err)
		tracker = history.NewNoOpTracker()
	}
	return suggestions.NewMultiSourceSuggestionService(ctx, infra_persistence.NewDomainFS(fs), tracker, recentHistory, stderr, wp)
}
