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

func setupPolicyTest(t *testing.T) (*SecurityManager, *policyTool, context.Context) {
	tempDir := t.TempDir()
	sm := NewSecurityManager(&MockInteractor{Answer: "y"})
	sm.SetSafePathsFile(filepath.Join(tempDir, "safepaths.json"))
	sm.SetReadOnlyPathsFile(filepath.Join(tempDir, "readonlypaths.json"))

	p := newPolicyTool(sm)
	ctx := context.Background()
	return sm, p, ctx
}

func TestPolicyTool_SafePathManagement(t *testing.T) {
	t.Parallel()

	t.Run("Register Safe Path", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
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
		for _, sp := range sm.GetSafePaths() {
			if sp == absPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("path not in safe list")
		}
	})

	t.Run("List Safe Paths", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		// Register a path first so the list is not empty
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/list-test", "reason": "test"})

		res, _ := p.ListSafePaths(ctx, nil)
		if !strings.Contains(res.Text, "authorized safe paths") {
			t.Error("Expected safe paths in list")
		}
	})

	t.Run("Remove Safe Path", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		path := "/tmp/safe_to_remove"
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"})
		_, _ = p.RemoveSafePath(ctx, map[string]interface{}{"path": path})

		found := false
		absPath, _ := filepath.Abs(path)
		for _, sp := range sm.GetSafePaths() {
			if sp == absPath {
				found = true
				break
			}
		}
		if found {
			t.Errorf("path still in safe list after removal")
		}
	})
}

func TestPolicyTool_ReadPathManagement(t *testing.T) {
	t.Parallel()

	t.Run("Register and Remove Read Path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
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

	t.Run("List Empty Read Paths", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		res, _ := p.ListReadPaths(ctx, nil)
		if !strings.Contains(res.Text, "No additional read-only paths") {
			t.Error("Expected no read-only paths message")
		}
	})
}

func TestPolicyTool_BypassManagement(t *testing.T) {
	t.Parallel()
	sm, p, ctx := setupPolicyTest(t)

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
}

func TestPolicyTool_ValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("RegisterSafePath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RegisterSafePath(ctx, map[string]interface{}{"reason": "test"})
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("RemoveSafePath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RemoveSafePath(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("RegisterReadPath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RegisterReadPath(ctx, map[string]interface{}{"reason": "test"})
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("RemoveReadPath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RemoveReadPath(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})
}

func TestPolicyTool_DeniedInteractions(t *testing.T) {
	t.Parallel()

	t.Run("RegisterSafePath denied", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/denied", "reason": "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "denied") {
			t.Errorf("Expected denied message, got %q", res.Text)
		}
	})

	t.Run("BypassConfirmation denied", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, err := p.BypassConfirmation(ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "denied") {
			t.Errorf("Expected denied message, got %q", res.Text)
		}
	})
}

func TestPolicyTool_BypassBehavior(t *testing.T) {
	t.Parallel()

	t.Run("RegisterSafePath bypass", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetBypassActive(true)
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/b1", "reason": "r"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(res.Text, "denied") {
			t.Errorf("Expected success, got denied")
		}
	})

	t.Run("RemoveSafePath bypass", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetBypassActive(true)
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, err := p.RemoveSafePath(ctx, map[string]interface{}{"path": "/tmp/b1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(res.Text, "denied") {
			t.Errorf("Expected success, got denied")
		}
	})

	t.Run("RegisterReadPath bypass", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetBypassActive(true)
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, err := p.RegisterReadPath(ctx, map[string]interface{}{"path": "/tmp/b2", "reason": "r"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(res.Text, "denied") {
			t.Errorf("Expected success, got denied")
		}
	})

	t.Run("RemoveReadPath bypass", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetBypassActive(true)
		sm.SetInteractor(&MockInteractor{Answer: "n"})
		res, err := p.RemoveReadPath(ctx, map[string]interface{}{"path": "/tmp/b2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(res.Text, "denied") {
			t.Errorf("Expected success, got denied")
		}
	})
}

func TestPolicy_Register(t *testing.T) {
	t.Parallel()
	sm, _, _ := setupPolicyTest(t)
	r := registry.New()
	if err := RegisterPolicy(r, sm); err != nil {
		t.Fatalf("RegisterPolicy failed: %v", err)
	}
}
