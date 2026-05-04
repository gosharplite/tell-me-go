// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"io"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// Bootstrapper defines the infrastructure interface for resolving dependencies.
// This matches the methods provided by the DI container.
type Bootstrapper interface {
	agent.SessionLifecycleManager
	ports.HistoryManagerProvider
	ports.SuggestionProvider
	GetAgentFactory() ports.ChatterFactory
	GetUIRenderer() ports.UIRenderer
	GetHistoryRenderer() ports.HistoryRenderer
	GetHistoryBrowser() ports.HistoryBrowser
	GetChatService() agent.ChatService
}

// context provides shared dependencies for commands.
type context struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	HomeDir      string
	SM           domain_security.Manager
	ChatService  agent.ChatService
	Bootstrapper Bootstrapper
	Loader       domain_config.ConfigLoader
	MockPrompt   string
	MockAnswer   string
	Interactor   *domain_security.UserInteractor
}
