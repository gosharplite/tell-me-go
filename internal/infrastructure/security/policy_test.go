// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
	sm, p, ctx := setupPolicyTest(t)

	t.Run("Register Safe Path", func(t *testing.T) {
		t.Parallel()
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
		res, _ := p.ListSafePaths(ctx, nil)
		if !strings.Contains(res.Text, "authorized safe paths") {
			t.Error("Expected safe paths in list")
		}
	})

	t.Run("Remove Safe Path", func(t *testing.T) {
		t.Parallel()
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
	_, p, ctx := setupPolicyTest(t)

	t.Run("Register and Remove Read Path", func(t *testing.T) {
		t.Parallel()
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
	_, p, ctx := setupPolicyTest(t)

	tests := []struct {
		name   string
		method func(context.Context, map[string]interface{}) (tools.ToolResult, error)
		args   map[string]interface{}
	}{
		{"RegisterSafePath missing path", p.RegisterSafePath, map[string]interface{}{"reason": "test"}},
		{"RemoveSafePath missing path", p.RemoveSafePath, map[string]interface{}{}},
		{"RegisterReadPath missing path", p.RegisterReadPath, map[string]interface{}{"reason": "test"}},
		{"RemoveReadPath missing path", p.RemoveReadPath, map[string]interface{}{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.method(ctx, tt.args)
			if err == nil {
				t.Error("Expected error for missing path")
			}
		})
	}
}

func TestPolicyTool_DeniedInteractions(t *testing.T) {
	t.Parallel()
	sm, p, ctx := setupPolicyTest(t)
	sm.SetInteractor(&MockInteractor{Answer: "n"})

	tests := []struct {
		name   string
		method func(context.Context, map[string]interface{}) (tools.ToolResult, error)
		args   map[string]interface{}
	}{
		{"RegisterSafePath denied", p.RegisterSafePath, map[string]interface{}{"path": "/tmp/denied", "reason": "test"}},
		{"BypassConfirmation denied", p.BypassConfirmation, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := tt.method(ctx, tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(res.Text, "denied") {
				t.Errorf("Expected denied message, got %q", res.Text)
			}
		})
	}
}

func TestPolicyTool_BypassBehavior(t *testing.T) {
	t.Parallel()
	sm, p, ctx := setupPolicyTest(t)
	sm.SetBypassActive(true)

	// Even if interactor says "n", bypass should let it through.
	sm.SetInteractor(&MockInteractor{Answer: "n"})

	tests := []struct {
		name   string
		method func(context.Context, map[string]interface{}) (tools.ToolResult, error)
		args   map[string]interface{}
	}{
		{"RegisterSafePath bypass", p.RegisterSafePath, map[string]interface{}{"path": "/tmp/b1", "reason": "r"}},
		{"RemoveSafePath bypass", p.RemoveSafePath, map[string]interface{}{"path": "/tmp/b1"}},
		{"RegisterReadPath bypass", p.RegisterReadPath, map[string]interface{}{"path": "/tmp/b2", "reason": "r"}},
		{"RemoveReadPath bypass", p.RemoveReadPath, map[string]interface{}{"path": "/tmp/b2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := tt.method(ctx, tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Contains(res.Text, "denied") {
				t.Errorf("Expected success, got denied")
			}
		})
	}
}

func TestPolicy_Register(t *testing.T) {
	t.Parallel()
	sm, _, _ := setupPolicyTest(t)
	r := registry.New()
	RegisterPolicy(r, sm)
}
