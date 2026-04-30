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
	HomeDir      string
	Version      string
	Stdout       io.Writer
	Stderr       io.Writer
	SM           ConfigurableSecurityManager
	FileSystem   infra_persistence.FileSystem
	Bootstrapper *Bootstrapper
}

func newChatFactory(homeDir, version string, stdout, stderr io.Writer, sm ConfigurableSecurityManager, fs infra_persistence.FileSystem, b *Bootstrapper) chatFactory {
	return &defaultChatFactory{
		HomeDir:      homeDir,
		Version:      version,
		Stdout:       stdout,
		Stderr:       stderr,
		SM:           sm,
		FileSystem:   fs,
		Bootstrapper: b,
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
		f.Bootstrapper,
		f.Bootstrapper.GetAgentFactory(),
		f.Bootstrapper.GetUIRenderer(),
		f.Bootstrapper.GetHistoryRenderer(),
		f.Bootstrapper.GetHistoryBrowser(),
		infra_persistence.NewDomainFS(f.FileSystem),
	)
}
