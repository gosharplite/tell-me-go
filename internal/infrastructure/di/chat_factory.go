// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"io"

	"github.com/gosharplite/tell-me-go/internal/app"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type chatFactory interface {
	AgentFactory() ports.ChatterFactory
	ChatService() ports.ChatService
}

type defaultChatFactory struct {
	HomeDir          string
	Version          string
	Stdout           io.Writer
	Stderr           io.Writer
	SM               ConfigurableSecurityManager
	FileSystem       infra_persistence.FileSystem
	LifecycleManager ports.SessionLifecycleManager
	UIFact           uiFactory
}

func newChatFactory(homeDir, version string, stdout, stderr io.Writer, sm ConfigurableSecurityManager, fs infra_persistence.FileSystem, lifecycleManager ports.SessionLifecycleManager, uiFact uiFactory) chatFactory {
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
	return app.NewChatter
}

func (f *defaultChatFactory) ChatService() ports.ChatService {
	return app.NewChatService(ports.ChatServiceConfig{
		HomeDir:          f.HomeDir,
		Version:          f.Version,
		Stdout:           f.Stdout,
		Stderr:           f.Stderr,
		SM:               f.SM,
		LifecycleManager: f.LifecycleManager,
		ChatterFactory:   f.AgentFactory(),
		UIRenderer:       f.UIFact.UIRenderer(),
		HistoryRenderer:  f.UIFact.HistoryRenderer(),
		HistoryBrowser:   f.UIFact.HistoryBrowser(),
		HistoryEditor:    f.UIFact.HistoryEditor(),
		LogOpener:        infra_persistence.NewDomainFS(f.FileSystem),
	})
}
