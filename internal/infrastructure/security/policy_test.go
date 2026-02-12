// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

func TestPolicyTool(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSecurityManager(&MockInteractor{Answer: "y"})
	sm.SetSafePathsFile(filepath.Join(tempDir, "safepaths.json"))
	sm.SetReadOnlyPathsFile(filepath.Join(tempDir, "readonlypaths.json"))

	p := newPolicyTool(sm)
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
		absPath, _ := filepath.Abs(path)
		for _, p := range sm.GetSafePaths() {
			if p == absPath {
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

	t.Run("List Paths", func(t *testing.T) {
		res, _ := p.ListSafePaths(ctx, nil)
		if !strings.Contains(res.Text, "authorized safe paths") {
			t.Error("Expected safe paths in list")
		}

		res, _ = p.ListReadPaths(ctx, nil)
		if !strings.Contains(res.Text, "No additional read-only paths") {
			t.Error("Expected no read-only paths message")
		}
	})

	t.Run("Register and Remove Read Path", func(t *testing.T) {
		path := "/tmp/ro"
		_, _ = p.RegisterReadPath(ctx, map[string]interface{}{"path": path, "reason": "test"})
		
		res, _ := p.ListReadPaths(ctx, nil)
		if !strings.Contains(res.Text, path) {
			t.Error("Expected read-only path in list")
		}

		_, _ = p.RemoveReadPath(ctx, map[string]interface{}{"path": path})
		res, _ = p.ListReadPaths(ctx, nil)
		if strings.Contains(res.Text, path) {
			t.Error("Did not expect read-only path in list after removal")
		}
	})

	t.Run("Remove Safe Path", func(t *testing.T) {
		path := "/tmp/safe_to_remove"
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"})
		_, _ = p.RemoveSafePath(ctx, map[string]interface{}{"path": path})
	})
	
	t.Run("Error Cases", func(t *testing.T) {
		_, err := p.RegisterSafePath(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing path")
		}
		
		_, err = p.RemoveSafePath(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing path")
		}
		
		_, err = p.RegisterReadPath(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing path")
		}
		
		_, err = p.RemoveReadPath(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("Denied Cases", func(t *testing.T) {
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, _ := p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/denied", "reason": "test"})
		if !strings.Contains(res.Text, "denied") {
			t.Error("Expected denied message")
		}
		
		res, _ = p.BypassConfirmation(ctx, nil)
		if !strings.Contains(res.Text, "denied") {
			t.Error("Expected denied message for bypass")
		}
	})

	t.Run("Bypass Messages", func(t *testing.T) {
		sm.SetBypassActive(true)
		// RegisterSafePath bypass
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/b1", "reason": "r"})
		// RemoveSafePath bypass
		_, _ = p.RemoveSafePath(ctx, map[string]interface{}{"path": "/tmp/b1"})
		// RegisterReadPath bypass
		_, _ = p.RegisterReadPath(ctx, map[string]interface{}{"path": "/tmp/b2", "reason": "r"})
		// RemoveReadPath bypass
		_, _ = p.RemoveReadPath(ctx, map[string]interface{}{"path": "/tmp/b2"})
	})

	t.Run("RegisterPolicy", func(t *testing.T) {
		r := registry.New()
		RegisterPolicy(r, sm)
	})
}


