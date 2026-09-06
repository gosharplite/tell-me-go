// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"io"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// Bootstrapper defines the infrastructure interface for resolving dependencies.
// This matches the methods provided by the DI container.
type Bootstrapper interface {
	ports.SessionLifecycleManager
	ports.HistoryManagerProvider
	ports.SuggestionProvider
	GetAgentFactory() ports.ChatterFactory
	GetUIRenderer() ports.UIRenderer
	GetHistoryRenderer() ports.HistoryRenderer
	GetHistoryBrowser() ports.HistoryBrowser
	GetChatService() ports.ChatService
	GetSystemMetricsProvider() ports.SystemMetricsProvider
}

// context provides shared dependencies for commands.
type context struct {
	Version          string
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	HomeDir          string
	SM               domain_security.Manager
	ChatService      ports.ChatService
	Bootstrapper     Bootstrapper
	Loader           domain_config.ConfigLoader
	MockPrompt       string
	MockAnswer       string
	Interactor       *InteractorRef
	CallbackNotifier domain_callback.CallbackNotifier
	ModeLocker       domain_persistence.ModeLocker
}
