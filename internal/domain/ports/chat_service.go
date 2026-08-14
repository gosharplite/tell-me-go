// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"io"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// CapturerInteractor combines the capabilities of a UI capturer and a security user interactor.
// This ensures compile-time safety by requiring both interfaces.
type CapturerInteractor interface {
	Capturer
	domain_security.UserInteractor
}

// ChatCommand defines the intent and configuration for a chat session.
type ChatCommand struct {
	ConfigPath       string
	NewSession       bool
	LastN            int
	BackN            int
	RawOutput        bool
	UseTUIPrompt     bool
	TUIOutput        bool
	ProgressRenderer ProgressRenderer
	Retry            bool
	Prompt           string
}

// ChatService defines the interface for chat orchestration operations.
type ChatService interface {
	// ProcessMessage handles the entire business flow of a chat turn, including
	// dependency management, history loading, and session finalization.
	ProcessMessage(ctx context.Context, cfg *config.Config, cmd ChatCommand, capturer CapturerInteractor) error

	// GetLastUserMessage retrieves the last user message and the number of turns
	// to rollback to reach that point in history.
	GetLastUserMessage(ctx context.Context, hManager HistoryManager) (msg string, turnsToRollback int, err error)

	// BrowseHistory starts the TUI browser for chat history.
	BrowseHistory(ctx context.Context, provider UnifiedHistoryProvider, hManager HistoryManager) error

	// GetToolNames retrieves the names of all available tools.
	GetToolNames(ctx context.Context, reg tools.Registry) ([]string, error)

	// StreamTurnsLog resolves the turns log path for the current mode and streams it to the provided writer.
	StreamTurnsLog(ctx context.Context, cfg *config.Config, out io.Writer) error

	// RunDiagnostics performs a comprehensive system health check.
	RunDiagnostics(ctx context.Context, cfg *config.Config, configPath string, jsonOutput bool) error

	// EditLastTurn launches an interactive TUI to edit the last model turn's
	// text and thought content. It blocks until the editor is dismissed.
	EditLastTurn(ctx context.Context, hManager HistoryManager) error

	// UpdateLastTurn replaces the text of the last model turn, or deletes
	// the turn entirely when text is empty (useful for refusal recovery).
	UpdateLastTurn(ctx context.Context, hManager HistoryManager, text string) error
}

// SessionLifecycleManager defines the interface for building and finalizing sessions.
type SessionLifecycleManager interface {
	BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer CapturerInteractor) (ChatterComposer, HistoryManager, func(context.Context) error, error)
	FinalizeSession(ctx context.Context, hManager HistoryManager, deps SessionFinalizer, cfg *config.Config) error
}

// LogFileOpener defines the minimal interface required to open session log files.
type LogFileOpener interface {
	Open(ctx context.Context, name string) (persistence.File, error)
}

// ChatServiceConfig configures a chatService during construction.
type ChatServiceConfig struct {
	HomeDir string
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	SM      domain_security.Manager

	LifecycleManager SessionLifecycleManager
	ChatterFactory   ChatterFactory
	UIRenderer       UIRenderer
	HistoryRenderer  HistoryRenderer
	HistoryBrowser   HistoryBrowser
	HistoryEditor    HistoryEditor
	LogOpener        LogFileOpener

	// ResolvePaths resolves session filesystem paths. Defaults to persistence.ResolvePaths.
	// Injectable for testing the empty-path defensive guard in StreamTurnsLog.
	ResolvePaths func(homeDir, mode string) *persistence.Paths
}
