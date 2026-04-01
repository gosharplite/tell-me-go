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
	if state.GetTasks() == nil {
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

func TestSessionState_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// 1. Create a session and set info
	state1, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	info := state1.GetInfo()
	info.ActiveToolkits = []string{"git", "k8s"}
	state1.SetInfo(info)
	_ = state1.Close()

	// 2. Reload the session state from disk
	state2, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state2.Close()

	// 3. Verify ActiveToolkits is restored
	restoredInfo := state2.GetInfo()
	if len(restoredInfo.ActiveToolkits) != 2 {
		t.Fatalf("expected 2 active toolkits, got %d", len(restoredInfo.ActiveToolkits))
	}
	
	tkMap := make(map[string]bool)
	for _, tk := range restoredInfo.ActiveToolkits {
		tkMap[tk] = true
	}
	
	if !tkMap["git"] || !tkMap["k8s"] {
		t.Errorf("restored toolkits missing git or k8s: %v", restoredInfo.ActiveToolkits)
	}
}
