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

	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	if state.Tasks == nil || state.Config == nil || state.Scratchpad == nil {
		t.Error("expected all services to be initialized")
	}

	if state.Info.Paths["config_dir"] != tempDir {
		t.Errorf("expected config_dir to be %s, got %s", tempDir, state.Info.Paths["config_dir"])
	}
}
