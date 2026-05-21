// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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

func setupPolicyTestWithAnswer(t *testing.T, answer string) (*SecurityManager, *policyTool, context.Context) {
	t.Helper()
	mockKV := new(mockKVStore)
	mockKV.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	sm := NewSecurityManager(func() domain.UserInteractor { return &mockInteractor{Answer: answer} })
	p, err := newPolicyTool(sm, mockKV)
	if err != nil {
		t.Fatalf("failed to create policyTool: %v", err)
	}
	return sm, p, context.Background()
}

func setupPolicyTest(t *testing.T) (*SecurityManager, *policyTool, context.Context) {
	return setupPolicyTestWithAnswer(t, "y")
}

// setupPolicyWithKVError creates a SecurityManager, policyTool, and context where
// the KVStore's Set method returns the given error (not .Maybe() — the error
// is expected exactly once during the test).
func setupPolicyWithKVError(t *testing.T, err error) (*SecurityManager, *policyTool, context.Context) {
	t.Helper()
	mockKV := new(mockKVStore)
	mockKV.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(err)
	sm := NewSecurityManager(func() domain.UserInteractor { return &mockInteractor{Answer: "y"} })
	p, pErr := newPolicyTool(sm, mockKV)
	if pErr != nil {
		t.Fatalf("failed to create policyTool: %v", pErr)
	}
	return sm, p, context.Background()
}

// setupPolicyWithInteractorError creates a SecurityManager, policyTool, and context
// where the UserInteractor returns the given error on Confirm().
// The KVStore uses .Maybe() since the confirm error prevents reaching persistence.
func setupPolicyWithInteractorError(t *testing.T, err error) (*SecurityManager, *policyTool, context.Context) {
	t.Helper()
	mockKV := new(mockKVStore)
	mockKV.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	mi := &mockInteractor{Answer: "y", Err: err}
	sm := NewSecurityManager(func() domain.UserInteractor { return mi })
	p, pErr := newPolicyTool(sm, mockKV)
	if pErr != nil {
		t.Fatalf("failed to create policyTool: %v", pErr)
	}
	return sm, p, context.Background()
}

// setupPolicyWithKVRemoveError creates a SecurityManager, policyTool, and context
// where the KVStore succeeds on the first Set (for Register) and fails on the
// second Set (for Remove). Uses .Once() to enforce ordered consumption.
// The caller must still call Register*Path to consume the first expectation.
func setupPolicyWithKVRemoveError(t *testing.T, err error) (*SecurityManager, *policyTool, context.Context) {
	t.Helper()
	mockKV := new(mockKVStore)
	// First Set: consumed by Register*Path → must succeed
	mockKV.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	// Second Set: consumed by Remove*Path → returns the given error
	mockKV.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(err).Once()
	sm := NewSecurityManager(func() domain.UserInteractor { return &mockInteractor{Answer: "y"} })
	p, pErr := newPolicyTool(sm, mockKV)
	if pErr != nil {
		t.Fatalf("failed to create policyTool: %v", pErr)
	}
	return sm, p, context.Background()
}

func TestPolicyTool_SafePathManagement_Register(t *testing.T) {
	t.Parallel()

	t.Run("Register Safe Path", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		path := filepath.Join(t.TempDir(), "safe")
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

	t.Run("Register Safe Path persist error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVError(t, fmt.Errorf("persist failed"))
		path := filepath.Join(t.TempDir(), "safe-persist-err")
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "failed to persist") {
			t.Errorf("Expected persist error message, got %q", res.Text)
		}
	})

	t.Run("Register Safe Path confirm error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithInteractorError(t, fmt.Errorf("confirm failed"))
		_, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": filepath.Join(t.TempDir(), "safe-confirm-err"), "reason": "test"}, nil)
		if err == nil {
			t.Error("expected error from confirm failure")
		}
	})
}

func TestPolicyTool_SafePathManagement_List(t *testing.T) {
	t.Parallel()

	t.Run("List Safe Paths", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		path := filepath.Join(t.TempDir(), "list-test")
		_, _ = p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)

		res, _ := p.ListSafePaths(ctx, nil, nil)
		if !strings.Contains(res.Text, "authorized safe paths") {
			t.Error("Expected safe paths in list")
		}
	})

	t.Run("List Safe Paths empty", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		res, _ := p.ListSafePaths(ctx, nil, nil)
		if !strings.Contains(res.Text, "No additional safe paths") {
			t.Errorf("Expected empty message, got %q", res.Text)
		}
	})
}

func TestPolicyTool_SafePathManagement_Remove(t *testing.T) {
	t.Parallel()

	t.Run("Remove Safe Path", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		path := filepath.Join(t.TempDir(), "safe_to_remove")
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

	t.Run("Remove Safe Path not found", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		path := filepath.Join(t.TempDir(), "nonexistent_to_remove")
		res, err := p.RemoveSafePath(ctx, map[string]interface{}{"path": path}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "Error:") && !strings.Contains(res.Text, "not found") {
			t.Errorf("Expected not-found error message, got %q", res.Text)
		}
	})

	t.Run("Remove Safe Path persist error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVRemoveError(t, fmt.Errorf("persist failed"))
		path := filepath.Join(t.TempDir(), "safe-rm-persist")
		_, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}

		res, err := p.RemoveSafePath(ctx, map[string]interface{}{"path": path}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "failed to update persistence") {
			t.Errorf("Expected persist error message, got %q", res.Text)
		}
	})
}

func TestPolicyTool_ReadPathManagement(t *testing.T) {
	t.Parallel()

	t.Run("Register and Remove Read Path", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		path := filepath.Join(t.TempDir(), "ro")
		_, _ = p.RegisterReadPath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)

		res, _ := p.ListReadPaths(ctx, nil, nil)

		// Use filepath.Abs to ensure we compare absolute paths consistently
		absPath, _ := filepath.Abs(path)
		if !strings.Contains(res.Text, absPath) && !strings.Contains(res.Text, path) {
			t.Errorf("Expected read-only path %q in list: %s", absPath, res.Text)
		}

		_, _ = p.RemoveReadPath(ctx, map[string]interface{}{"path": path}, nil)
		res, _ = p.ListReadPaths(ctx, nil, nil)
		if strings.Contains(res.Text, absPath) {
			t.Errorf("Did not expect read-only path %q in list after removal", absPath)
		}
	})

	t.Run("Register Read Path persist error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVError(t, fmt.Errorf("persist failed"))
		path := filepath.Join(t.TempDir(), "ro-persist-err")
		res, err := p.RegisterReadPath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "failed to persist") {
			t.Errorf("Expected persist error message, got %q", res.Text)
		}
	})

	t.Run("Remove Read Path persist error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVRemoveError(t, fmt.Errorf("persist failed"))
		path := filepath.Join(t.TempDir(), "ro-rm-persist")
		_, err := p.RegisterReadPath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}

		res, err := p.RemoveReadPath(ctx, map[string]interface{}{"path": path}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "failed to update persistence") {
			t.Errorf("Expected persist error message, got %q", res.Text)
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

func TestPolicyTool_SessionSettings_Update(t *testing.T) {
	t.Parallel()

	t.Run("Update Session Setting", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		mockKV := p.kv.(*mockKVStore)
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

	t.Run("Update Session Setting missing key", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		_, err := p.UpdateSessionSetting(ctx, map[string]interface{}{
			"value": "test_val",
		}, nil)
		if err == nil {
			t.Error("expected error for missing key")
		}
	})

	t.Run("Update Session Setting denied", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTestWithAnswer(t, "n")
		res, err := p.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "some_key",
			"value": "some_val",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "denied") {
			t.Errorf("Expected denied message, got %q", res.Text)
		}
	})

	t.Run("Update Session Setting confirm error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithInteractorError(t, fmt.Errorf("confirm failed"))
		_, err := p.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "some_key",
			"value": "some_val",
		}, nil)
		if err == nil {
			t.Error("expected error from confirm failure")
		}
	})

	t.Run("Update Session Setting Set error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVError(t, fmt.Errorf("persist failed"))
		_, err := p.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "some_key",
			"value": "some_val",
		}, nil)
		if err == nil {
			t.Error("expected error from Set failure")
		}
	})
}

func TestPolicyTool_SessionSettings_List(t *testing.T) {
	t.Parallel()

	t.Run("List Session Settings", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		mockKV := p.kv.(*mockKVStore)
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

	t.Run("List Session Settings empty", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		mockKV := p.kv.(*mockKVStore)
		mockKV.On("GetAll", ctx).Return(map[string]string{}, nil)

		res, err := p.ListSessionSettings(ctx, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "No persistent session settings") {
			t.Errorf("Expected empty message, got %q", res.Text)
		}
		mockKV.AssertExpectations(t)
	})

	t.Run("List Session Settings error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTest(t)
		mockKV := p.kv.(*mockKVStore)
		mockKV.On("GetAll", ctx).Return(map[string]string(nil), fmt.Errorf("storage error"))

		_, err := p.ListSessionSettings(ctx, nil, nil)
		if err == nil {
			t.Error("expected error from GetAll")
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

	t.Run("RegisterReadPath confirm error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithInteractorError(t, fmt.Errorf("confirm failed"))
		_, err := p.RegisterReadPath(ctx, map[string]interface{}{"path": filepath.Join(t.TempDir(), "ro-confirm-err"), "reason": "test"}, nil)
		if err == nil {
			t.Error("expected error from confirm failure")
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
		_, p, ctx := setupPolicyTestWithAnswer(t, "n")
		path := filepath.Join(t.TempDir(), "denied")
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": path, "reason": "test"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "denied") {
			t.Errorf("Expected denied message, got %q", res.Text)
		}
	})

	t.Run("BypassConfirmation denied", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyTestWithAnswer(t, "n")
		res, err := p.BypassConfirmation(ctx, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "denied") {
			t.Errorf("Expected denied message, got %q", res.Text)
		}
	})

	t.Run("BypassConfirmation already active", func(t *testing.T) {
		t.Parallel()
		sm, p, ctx := setupPolicyTest(t)
		sm.SetBypassActive(true)
		res, err := p.BypassConfirmation(ctx, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "already enabled") {
			t.Errorf("Expected 'already enabled', got %q", res.Text)
		}
	})

	t.Run("BypassConfirmation Set error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVError(t, fmt.Errorf("persist failed"))
		_, err := p.BypassConfirmation(ctx, nil, nil)
		if err == nil {
			t.Error("expected error from Set failure in BypassConfirmation")
		}
	})

	t.Run("RevokeBypass Set error", func(t *testing.T) {
		t.Parallel()
		_, p, ctx := setupPolicyWithKVError(t, fmt.Errorf("persist failed"))
		_, err := p.RevokeBypass(ctx, nil, nil)
		if err == nil {
			t.Error("expected error from Set failure in RevokeBypass")
		}
	})
}

func assertBypassSuccess(t *testing.T, res tools.ToolResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(res.Text, "denied") {
		t.Errorf("Expected success, got denied")
	}
}

func TestPolicyTool_BypassBehavior(t *testing.T) {
	t.Parallel()
	sm, p, ctx := setupPolicyTestWithAnswer(t, "n")
	sm.SetBypassActive(true)

	t.Run("RegisterSafePath bypass", func(t *testing.T) {
		res, err := p.RegisterSafePath(ctx, map[string]interface{}{"path": filepath.Join(t.TempDir(), "b1"), "reason": "r"}, nil)
		assertBypassSuccess(t, res, err)
	})

	t.Run("RemoveSafePath bypass", func(t *testing.T) {
		res, err := p.RemoveSafePath(ctx, map[string]interface{}{"path": filepath.Join(t.TempDir(), "b1")}, nil)
		assertBypassSuccess(t, res, err)
	})

	t.Run("RegisterReadPath bypass", func(t *testing.T) {
		res, err := p.RegisterReadPath(ctx, map[string]interface{}{"path": filepath.Join(t.TempDir(), "b2"), "reason": "r"}, nil)
		assertBypassSuccess(t, res, err)
	})

	t.Run("RemoveReadPath bypass", func(t *testing.T) {
		res, err := p.RemoveReadPath(ctx, map[string]interface{}{"path": filepath.Join(t.TempDir(), "b2")}, nil)
		assertBypassSuccess(t, res, err)
	})
}

func TestPolicy_Register(t *testing.T) {
	t.Parallel()
	sm, p, _ := setupPolicyTest(t)
	r := registry.New()
	if err := sm.RegisterPolicyTools(r, p.kv); err != nil {
		t.Fatalf("RegisterPolicyTools failed: %v", err)
	}
}

func TestNewPolicyTool_ValidationErrors(t *testing.T) {
	t.Parallel()

	t.Run("nil SecurityManager", func(t *testing.T) {
		_, err := newPolicyTool(nil, new(mockKVStore))
		if err == nil {
			t.Error("expected error for nil SecurityManager")
		}
		if !strings.Contains(err.Error(), "SecurityManager") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil KVStore", func(t *testing.T) {
		sm := NewSecurityManager(nil)
		_, err := newPolicyTool(sm, nil)
		if err == nil {
			t.Error("expected error for nil KVStore")
		}
		if !strings.Contains(err.Error(), "KVStore") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
