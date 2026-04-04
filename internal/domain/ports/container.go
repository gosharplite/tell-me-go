// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
)

// Container defines the interface for building session dependencies and providing UI factories.
// This interface is implemented by the infrastructure layer (DI container) and used by the application layer.
type Container interface {
	// BuildSessionDependencies assembles all dependencies required for a chat session.
	BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (SessionDependencies, HistoryManager, func(context.Context) error, error)

	// GetAgentFactory returns a factory for creating Chatter instances.
	GetAgentFactory() ChatterFactory

	// FinalizeSession saves history and records session cost.
	FinalizeSession(ctx context.Context, hManager HistoryManager, deps SessionDependencies, cfg *config.Config) error

	// GetHistoryManager loads the history manager for a given configuration.
	GetHistoryManager(ctx context.Context, cfg *config.Config) (HistoryManager, error)

	// GetUnifiedHistoryProvider assembles the read-model for the history browser.
	GetUnifiedHistoryProvider(ctx context.Context, cfg *config.Config, hManager HistoryManager) (UnifiedHistoryProvider, error)

	// GetToolNames retrieves the names of all available tools without starting a full session.
	GetToolNames(ctx context.Context, cfg *config.Config, configPath string) ([]string, error)

	// GetSuggestionService initializes and returns the suggestion service.
	GetSuggestionService(ctx context.Context, recentHistory []string) (SuggestionService, error)

	// GetSystemMetricsProvider returns the system metrics provider based on the platform.
	GetSystemMetricsProvider() SystemMetricsProvider

	// GetUIRenderer returns a UI renderer configured with the output writers.
	GetUIRenderer() UIRenderer

	// GetHistoryRenderer returns a history renderer.
	GetHistoryRenderer() HistoryRenderer

	// GetHistoryBrowser returns a history browser that launches the TUI.
	GetHistoryBrowser() HistoryBrowser
}