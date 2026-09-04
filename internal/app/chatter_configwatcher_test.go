// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// depsWithConfigWatcher wraps the in-package mockSessionDeps to inject a
// non-nil ConfigWatcher, making the cw.SetPaths branch in NewChatter
// reachable in tests. mockSessionDeps always returns nil for
// GetConfigWatcher and must not be modified (its file carries a catalog
// pin), so the override lives here.
type depsWithConfigWatcher struct {
	*mockSessionDeps
	cw domain_config.ConfigWatcher
}

func (d *depsWithConfigWatcher) GetConfigWatcher() domain_config.ConfigWatcher { return d.cw }

var _ ports.ChatterComposer = (*depsWithConfigWatcher)(nil)

// recordingConfigWatcher is a race-safe ConfigWatcher double. Only SetPaths
// records; the other six methods are inert no-ops returning zero values —
// agent construction never calls them, only the applyConfig hot-reload
// chain does. The interface is NOT embedded: a nil-embedded interface would
// panic if any non-SetPaths method ever fired.
type recordingConfigWatcher struct {
	mu            sync.Mutex
	setPathsCalls [][2]string // recorded (main, session) pairs
}

func (w *recordingConfigWatcher) SetPaths(main, session string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.setPathsCalls = append(w.setPathsCalls, [2]string{main, session})
}

func (w *recordingConfigWatcher) Refresh(model string)                          {}
func (w *recordingConfigWatcher) SetLimits(tokens, toolTurns, historyTurns int) {}
func (w *recordingConfigWatcher) GetLimits() (int, int, int)                    { return 0, 0, 0 }
func (w *recordingConfigWatcher) GetContextWindow() int                         { return 0 }
func (w *recordingConfigWatcher) ApplyLimits(l events.Limits)                   {}
func (w *recordingConfigWatcher) GetMemoryConfig() domain_config.MemoryConfig {
	return domain_config.MemoryConfig{}
}

// TestNewChatter_ConfigWatcherSetPaths verifies that NewChatter wires the
// injected ConfigWatcher with the main config path and an empty session
// path: chatter.go calls cw.SetPaths(cfg.ConfigPath, "") — the session
// path is deliberately empty at chatter construction.
func TestNewChatter_ConfigWatcherSetPaths(t *testing.T) {
	deps, cfg := setupNilDepTest(t)
	cw := &recordingConfigWatcher{}
	cfg.ConfigPath = filepath.Join(t.TempDir(), "butler.yaml")

	chatter, err := NewChatter(context.Background(), &depsWithConfigWatcher{mockSessionDeps: deps, cw: cw}, cfg)
	if err != nil {
		t.Fatalf("NewChatter failed: %v", err)
	}
	if chatter == nil {
		t.Fatal("expected chatter to be non-nil")
	}

	cw.mu.Lock()
	defer cw.mu.Unlock()
	if len(cw.setPathsCalls) != 1 {
		t.Fatalf("SetPaths calls = %d, want 1", len(cw.setPathsCalls))
	}
	if got := cw.setPathsCalls[0]; got[0] != cfg.ConfigPath || got[1] != "" {
		t.Errorf("SetPaths(%q, %q), want (%q, \"\")", got[0], got[1], cfg.ConfigPath)
	}
}
