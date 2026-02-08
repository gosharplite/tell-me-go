// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestStateManager_GetSessionInfo(t *testing.T) {
	tempDir := t.TempDir()
	fs := fsutil.DefaultFileSystem
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)

	m := &stateManager{
		sm:         sm,
		tasks:      NewTaskStore(fs, filepath.Join(tempDir, "tasks.json")),
		config:     NewConfigStore(fs, filepath.Join(tempDir, "config.json")),
		scratchpad: NewScratchpadStore(fs, filepath.Join(tempDir, "scratchpad.md")),
	}

	ctx := context.Background()
	m.initSessionInfo(tempDir)

	t.Run("Get Session Info", func(t *testing.T) {
		if _, err := m.config.ManageConfig(ctx, map[string]interface{}{
			"action": "set",
			"key":    "test_key",
			"value":  "test_val",
		}); err != nil {
			t.Fatal(err)
		}

		res, err := m.getSessionInfo(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		var info SessionInfo
		if err := json.Unmarshal([]byte(res.Text), &info); err != nil {
			t.Fatal(err)
		}

		if info.Config["test_key"] != "test_val" {
			t.Errorf("expected test_val in session info, got %v", info.Config["test_key"])
		}
		if !strings.Contains(info.Paths["config_dir"], tempDir) {
			t.Errorf("expected config_dir to contain %s, got %s", tempDir, info.Paths["config_dir"])
		}
	})
}
