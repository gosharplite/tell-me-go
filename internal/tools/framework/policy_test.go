// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestPolicyTool(t *testing.T) {
	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	tempDir := t.TempDir()
	sm := security.NewSecurityManager(nil)
	sm.SetSafePathsFile(filepath.Join(tempDir, "safepaths.json"))

	p := NewPolicyTool(sm)
	ctx := context.Background()

	t.Run("Register Safe Path", func(t *testing.T) {
		path := "/tmp/safe"
		_, err := p.RegisterSafePath(ctx, map[string]interface{}{
			"path":   path,
			"reason": "testing",
		})
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, p := range sm.GetSafePaths() {
			if p == path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("path not in safe list")
		}
	})

	t.Run("Bypass Confirmation", func(t *testing.T) {
		sm.SetBypassActive(false)
		if sm.IsBypassActive() {
			t.Error("expected bypass to be inactive initially")
		}

		_, err := p.BypassConfirmation(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		if !sm.IsBypassActive() {
			t.Error("expected bypass to be active after call")
		}

		_, err = p.RevokeBypass(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}

		if sm.IsBypassActive() {
			t.Error("expected bypass to be inactive after revoke")
		}
	})
}
