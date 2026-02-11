// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"testing"
)

func TestNewSessionState(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Run("File Storage", func(t *testing.T) {
		state, err := NewSessionState(ctx, tempDir)
		if err != nil {
			t.Fatal(err)
		}

		if state.Tasks == nil || state.Config == nil || state.Scratchpad == nil {
			t.Error("expected all services to be initialized")
		}

		if state.Info.Env["STORAGE_TYPE"] != "file" {
			t.Errorf("expected STORAGE_TYPE to be file, got %s", state.Info.Env["STORAGE_TYPE"])
		}

		if state.Info.Paths["config_dir"] != tempDir {
			t.Errorf("expected config_dir to be %s, got %s", tempDir, state.Info.Paths["config_dir"])
		}
	})

	t.Run("Memory Storage", func(t *testing.T) {
		t.Setenv("STORAGE_TYPE", "memory")
		state, err := NewSessionState(ctx, tempDir)
		if err != nil {
			t.Fatal(err)
		}

		if state.Tasks == nil || state.Config == nil || state.Scratchpad == nil {
			t.Error("expected all services to be initialized")
		}

		if state.Info.Env["STORAGE_TYPE"] != "memory" {
			t.Errorf("expected STORAGE_TYPE to be memory, got %s", state.Info.Env["STORAGE_TYPE"])
		}

		// Should work without actual files
		if err := state.Config.Set(ctx, "mem_key", "mem_val"); err != nil {
			t.Fatal(err)
		}
		val, _ := state.Config.Get("mem_key")
		if val != "mem_val" {
			t.Errorf("expected mem_val, got %s", val)
		}
	})
}
