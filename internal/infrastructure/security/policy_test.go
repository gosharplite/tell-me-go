// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/stretchr/testify/mock"
)

type mockKVStore struct {
	mock.Mock
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}
func (m *mockKVStore) Set(ctx context.Context, key, val string) error {
	args := m.Called(ctx, key, val)
	return args.Error(0)
}
func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}
func (m *mockKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]string), args.Error(1)
}

type mockSessionProvider struct {
	mock.Mock
}

func (m *mockSessionProvider) GetTasks() ports.TaskStore { return nil }
func (m *mockSessionProvider) GetSettings() ports.KVStore {
	args := m.Called()
	return args.Get(0).(ports.KVStore)
}
func (m *mockSessionProvider) GetInfo() ports.SessionInfo { return ports.SessionInfo{} }
func (m *mockSessionProvider) SetInfo(info ports.SessionInfo) {}
func (m *mockSessionProvider) Close() error { return nil }

func setupPolicyTest(t *testing.T) (*SecurityManager, *policyTool, context.Context) {
	tempDir := t.TempDir()
	sm := NewSecurityManager(&MockInteractor{Answer: "y"})
	sm.SetSafePathsFile(filepath.Join(tempDir, "safepaths.json"))
	sm.SetReadOnlyPathsFile(filepath.Join(tempDir, "readonlypaths.json"))

	mockKV := new(mockKVStore)
	mockKV.On("Set", mock.Anything, "bypass_confirmation", mock.Anything).Return(nil).Maybe()
	mockSP := new(mockSessionProvider)
	mockSP.On("GetSettings").Return(mockKV).Maybe()

	p := newPolicyTool(sm, mockSP)
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
		}, nil)
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
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/list-test", "reason": "test"}, nil)

		res, _ := p.ListSafePaths(ctx, nil, nil)
		if !strings.Contains(res.Text, "authorized safe paths") {
			t.Error("Expected safe paths in list")
		}
	})

	t.Run("Remove Safe Path", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		path := "/tmp/safe_to_remove"
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)
		_, _ = p.RemoveSafePath(ctx, map[string]interface{}{"path": path}, nil)

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
		_, _ = p.RegisterReadPath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)

		res, _ := p.ListReadPaths(ctx, nil, nil)
		if !strings.Contains(res.Text, path) {
			t.Error("Expected read-only path in list")
		}

		_, _ = p.RemoveReadPath(ctx, map[string]interface{}{"path": path}, nil)
		res, _ = p.ListReadPaths(ctx, nil, nil)
		if strings.Contains(res.Text, path) {
			t.Error("Did not expect read-only path in list after removal")
		}
	})

	t.Run("List Empty Read Paths", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		res, _ := p.ListReadPaths(ctx, nil, nil)
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

	_, err := p.BypassConfirmation(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !sm.IsBypassActive() {
		t.Error("expected bypass to be active after call")
	}

	_, err = p.RevokeBypass(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if sm.IsBypassActive() {
		t.Error("expected bypass to be inactive after revoke")
	}
}

func TestPolicyTool_SessionSettings(t *testing.T) {
	t.Parallel()

	t.Run("Update Session Setting", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		mockKV := p.sp.GetSettings().(*mockKVStore)
		mockKV.On("Set", ctx, "test_key", "test_val").Return(nil)

		res, err := p.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "test_key",
			"value": "test_val",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "updated to 'test_val'") {
			t.Errorf("Unexpected result text: %q", res.Text)
		}
		mockKV.AssertExpectations(t)
	})

	t.Run("List Session Settings", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		mockKV := p.sp.GetSettings().(*mockKVStore)
		mockKV.On("GetAll", ctx).Return(map[string]string{"k1": "v1", "k2": "v2"}, nil)

		res, err := p.ListSessionSettings(ctx, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "k1") || !strings.Contains(res.Text, "v2") {
			t.Error("Expected settings k1 and v2 in list")
		}
		mockKV.AssertExpectations(t)
	})
}

func TestPolicyTool_ValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("RegisterSafePath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RegisterSafePath(ctx, map[string]interface{}{"reason": "test"}, nil)
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("RemoveSafePath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RemoveSafePath(ctx, map[string]interface{}{}, nil)
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("RegisterReadPath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RegisterReadPath(ctx, map[string]interface{}{"reason": "test"}, nil)
		if err == nil {
			t.Error("Expected error for missing path")
		}
	})

	t.Run("RemoveReadPath missing path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.RemoveReadPath(ctx, map[string]interface{}{}, nil)
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
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/denied", "reason": "test"}, nil)
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
		res, err := p.BypassConfirmation(ctx, nil, nil)
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
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": "/tmp/b1", "reason": "r"}, nil)
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
		res, err := p.RemoveSafePath(ctx, map[string]interface{}{"path": "/tmp/b1"}, nil)
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
		res, err := p.RegisterReadPath(ctx, map[string]interface{}{"path": "/tmp/b2", "reason": "r"}, nil)
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
		res, err := p.RemoveReadPath(ctx, map[string]interface{}{"path": "/tmp/b2"}, nil)
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
	sm, p, _ := setupPolicyTest(t)
	r := registry.New()
	if err := sm.RegisterPolicyTools(r, p.sp); err != nil {
		t.Fatalf("RegisterPolicyTools failed: %v", err)
	}
}
