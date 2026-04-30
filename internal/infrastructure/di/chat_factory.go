// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"io"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type chatFactory interface {
	AgentFactory() ports.ChatterFactory
	ChatService() agent.ChatService
}

type defaultChatFactory struct {
	HomeDir          string
	Version          string
	Stdout           io.Writer
	Stderr           io.Writer
	SM               ConfigurableSecurityManager
	FileSystem       infra_persistence.FileSystem
	LifecycleManager agent.SessionLifecycleManager
	UIFact           uiFactory
}

func newChatFactory(homeDir, version string, stdout, stderr io.Writer, sm ConfigurableSecurityManager, fs infra_persistence.FileSystem, lifecycleManager agent.SessionLifecycleManager, uiFact uiFactory) chatFactory {
	return &defaultChatFactory{
		HomeDir:          homeDir,
		Version:          version,
		Stdout:           stdout,
		Stderr:           stderr,
		SM:               sm,
		FileSystem:       fs,
		LifecycleManager: lifecycleManager,
		UIFact:           uiFact,
	}
}

func (f *defaultChatFactory) AgentFactory() ports.ChatterFactory {
	return factory.NewChatter
}

func (f *defaultChatFactory) ChatService() agent.ChatService {
	return agent.NewChatService(
		f.HomeDir,
		f.Version,
		f.Stdout,
		f.Stderr,
		f.SM,
		f.LifecycleManager,
		f.AgentFactory(),
		f.UIFact.UIRenderer(),
		f.UIFact.HistoryRenderer(),
		f.UIFact.HistoryBrowser(),
		infra_persistence.NewDomainFS(f.FileSystem),
	)
}
