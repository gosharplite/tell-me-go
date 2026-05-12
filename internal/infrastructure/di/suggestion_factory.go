// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

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

type suggestionFactory interface {
	BuildSuggestionService(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error)
}

type defaultSuggestionFactory struct {
	HomeDir         string
	FileSystem      infra_persistence.FileSystem
	Stderr          io.Writer
	Logger          *slog.Logger
	WorkspacePolicy services.WorkspacePolicy
}

func newSuggestionFactory(homeDir string, fs infra_persistence.FileSystem, stderr io.Writer, logger *slog.Logger, wp services.WorkspacePolicy) suggestionFactory {
	return &defaultSuggestionFactory{
		HomeDir:         homeDir,
		FileSystem:      fs,
		Stderr:          stderr,
		Logger:          logger,
		WorkspacePolicy: wp,
	}
}

func (f *defaultSuggestionFactory) BuildSuggestionService(ctx stdctx.Context, recentHistory []string) (ports.SuggestionService, error) {
	tracker, err := history.NewGlobalPromptTracker(infra_persistence.NewDomainFS(f.FileSystem), f.HomeDir)
	if err != nil {
		f.Logger.Warn("failed to initialize global prompt tracker, falling back to no-op", "error", err)
		tracker = history.NewNoOpTracker()
	}

	return suggestions.NewMultiSourceSuggestionService(ctx, infra_persistence.NewDomainFS(f.FileSystem), tracker, recentHistory, f.Stderr, f.WorkspacePolicy)
}
