// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	stdctx "context"
	"io"

	"github.com/gosharplite/tell-me-go/internal/application/suggestions"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// BuildSuggestionService constructs the suggestion service.
func BuildSuggestionService(ctx stdctx.Context, fs persistence.FileSystem, tracker ports.PromptTracker, recentHistory []string, stderr io.Writer, wp services.WorkspacePolicy) (ports.SuggestionService, error) {
	return suggestions.NewMultiSourceSuggestionService(ctx, fs, tracker, recentHistory, stderr, wp)
}
