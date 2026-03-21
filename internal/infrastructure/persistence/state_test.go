// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func verifyStateInitialization(t *testing.T, state ports.SessionProvider) {
	t.Helper()
	if state.GetTasks() == nil || state.GetScratchpad() == nil {
		t.Error("expected all services to be initialized")
	}
}

func TestNewSessionState_FileStorage(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	ctx := context.Background()

	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	verifyStateInitialization(t, state)

	info := state.GetInfo()
	if info.Env["STORAGE_TYPE"] != "sqlite" {
		t.Errorf("expected STORAGE_TYPE to be sqlite, got %s", info.Env["STORAGE_TYPE"])
	}

	if info.Paths["config_dir"] != tempDir {
		t.Errorf("expected config_dir to be %s, got %s", tempDir, info.Paths["config_dir"])
	}
}

func TestNewSessionState_MemoryStorage(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	verifyStateInitialization(t, state)

	info := state.GetInfo()
	if info.Env["STORAGE_TYPE"] != "memory" {
		t.Errorf("expected STORAGE_TYPE to be memory, got %s", info.Env["STORAGE_TYPE"])
	}
}
