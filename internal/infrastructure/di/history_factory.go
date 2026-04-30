// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	stdctx "context"
	"errors"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type historyFactory interface {
	BuildHistoryManager(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error)
	BuildUnifiedHistoryProvider(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error)
}

type defaultHistoryFactory struct {
	HomeDir    string
	FileSystem infra_persistence.FileSystem
}

func newHistoryFactory(homeDir string, fs infra_persistence.FileSystem) historyFactory {
	return &defaultHistoryFactory{
		HomeDir:    homeDir,
		FileSystem: fs,
	}
}

func (f *defaultHistoryFactory) BuildHistoryManager(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
	paths := persistence.ResolvePaths(f.HomeDir, cfg.Mode)
	if err := infra_persistence.EnsureDirectories(ctx, f.FileSystem, paths); err != nil {
		return nil, fmt.Errorf("%w: failed to ensure session directories for %s: %w", errInfraInit, cfg.Mode, err)
	}

	hManager := history.NewManager(infra_persistence.NewDomainFS(f.FileSystem), paths.HistoryPath, paths.HistoryArchivePath)
	if err := hManager.Load(ctx); err != nil {
		if !errors.Is(err, ports.ErrHistoryNotFound) {
			return nil, fmt.Errorf("%w: failed to load history from %s: %w", errInfraInit, paths.HistoryPath, err)
		}
	}

	return hManager, nil
}

func (f *defaultHistoryFactory) BuildUnifiedHistoryProvider(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	paths := persistence.ResolvePaths(f.HomeDir, cfg.Mode)
	if err := infra_persistence.EnsureDirectories(ctx, f.FileSystem, paths); err != nil {
		return nil, fmt.Errorf("%w: failed to ensure session directories for unified history: %w", errInfraInit, err)
	}

	archiveReader := history.NewJSONLArchiveReader(infra_persistence.NewDomainFS(f.FileSystem), paths.HistoryArchivePath)

	return history.NewUnifiedProvider(archiveReader, hManager), nil
}
