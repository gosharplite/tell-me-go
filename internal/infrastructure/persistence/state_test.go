// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

func verifyStateInitialization(t *testing.T, state services.ISessionProvider) {
	t.Helper()
	if state.GetTasks() == nil || state.GetConfig() == nil || state.GetScratchpad() == nil {
		t.Error("expected all services to be initialized")
	}
}

func TestNewSessionState_FileStorage(t *testing.T) {
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

	// Should work without actual files
	if err := state.GetConfig().Set(ctx, "mem_key", "mem_val"); err != nil {
		t.Fatal(err)
	}
	val, _ := state.GetConfig().Get("mem_key")
	if val != "mem_val" {
		t.Errorf("expected mem_val, got %s", val)
	}
}
