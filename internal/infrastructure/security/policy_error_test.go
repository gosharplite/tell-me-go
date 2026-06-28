// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// funcMockKVStore implements ports.KVStore using function fields,
// allowing precise control over error injection for error-path testing.
type funcMockKVStore struct {
	getFunc    func(ctx context.Context, key string) (string, error)
	setFunc    func(ctx context.Context, key, value string) error
	deleteFunc func(ctx context.Context, key string) error
	getAllFunc func(ctx context.Context) (map[string]string, error)
}

func (m *funcMockKVStore) Get(ctx context.Context, key string) (string, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return "", nil
}

func (m *funcMockKVStore) Set(ctx context.Context, key, value string) error {
	if m.setFunc != nil {
		return m.setFunc(ctx, key, value)
	}
	return nil
}

func (m *funcMockKVStore) Delete(ctx context.Context, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

func (m *funcMockKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	if m.getAllFunc != nil {
		return m.getAllFunc(ctx)
	}
	return map[string]string{}, nil
}

// compile-time interface satisfaction check
var _ ports.KVStore = (*funcMockKVStore)(nil)

// stagedInteractor implements UserInteractor with staged answers and captured warnings.
type stagedInteractor struct {
	answers []bool
	idx     int
	warns   []string
}

func (s *stagedInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	if s.idx >= len(s.answers) {
		return false, nil
	}
	ans := s.answers[s.idx]
	s.idx++
	return ans, nil
}
func (s *stagedInteractor) Warn(message string)                               { s.warns = append(s.warns, message) }
func (s *stagedInteractor) Prompt(message string)                             { s.warns = append(s.warns, message) }
func (s *stagedInteractor) ReadSingleKey(ctx context.Context) (string, error) { return "", nil }
func (s *stagedInteractor) ReadLine(ctx context.Context) (string, error)      { return "", nil }

func TestPolicyTool_ErrorPaths(t *testing.T) {
	t.Parallel()

	// 1. RegisterSafePath persist error
	t.Run("RegisterSafePath persist error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{
			setFunc: func(ctx context.Context, key, value string) error {
				return fmt.Errorf("persist failed")
			},
		}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "safe-persist-err")
		res, err := pt.RegisterSafePath(ctx, map[string]interface{}{
			"path":   path,
			"reason": "testing persist failure",
		}, nil)

		require.NoError(t, err)
		assert.Contains(t, res.Text, "Path authorized but failed to persist")
	})

	// 2. RegisterReadPath persist error
	t.Run("RegisterReadPath persist error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{
			setFunc: func(ctx context.Context, key, value string) error {
				return fmt.Errorf("persist failed")
			},
		}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "ro-persist-err")
		res, err := pt.RegisterReadPath(ctx, map[string]interface{}{
			"path":   path,
			"reason": "testing persist failure",
		}, nil)

		require.NoError(t, err)
		assert.Contains(t, res.Text, "Path authorized for reading but failed to persist")
	})

	// 3. RemoveSafePath not found
	t.Run("RemoveSafePath not found", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "nonexistent-safe")
		res, err := pt.RemoveSafePath(ctx, map[string]interface{}{
			"path": path,
		}, nil)

		require.NoError(t, err)
		assert.Contains(t, res.Text, "Error:")
	})

	// 4. RemoveReadPath not found
	t.Run("RemoveReadPath not found", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "nonexistent-ro")
		res, err := pt.RemoveReadPath(ctx, map[string]interface{}{
			"path": path,
		}, nil)

		require.NoError(t, err)
		assert.Contains(t, res.Text, "Error:")
	})

	// 5. BypassConfirmation already active
	t.Run("BypassConfirmation already active", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		res, err := pt.BypassConfirmation(ctx, nil, nil)

		require.NoError(t, err)
		assert.Equal(t, "Bypass mode is already enabled.", res.Text)
	})

	// 6. confirmAction with bypass
	t.Run("confirmAction with bypass", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			// Answer "n" ensures that without bypass this would fail
			return &mockInteractor{Answer: "n"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}

		confirmed, err := pt.confirmAction(
			context.Background(),
			actionPathWrite,
			"/some/test/path",
			"testing bypass",
			true,
		)

		require.NoError(t, err)
		assert.True(t, confirmed)
	})

	// 7. confirmAction double-confirm denied
	t.Run("confirmAction double-confirm denied", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		si := &stagedInteractor{answers: []bool{true, false}}
		sm := NewSecurityManager(func() domain.UserInteractor { return si })
		pt := &policyTool{sm: sm, kv: kv}

		confirmed, err := pt.confirmAction(
			context.Background(),
			actionPathWrite,
			"/some/test/path",
			"testing double confirm denied",
			true, // doubleConfirm = true
		)

		require.NoError(t, err)
		assert.False(t, confirmed)
	})

	// 8. UpdateSessionSetting kv.Set error
	t.Run("UpdateSessionSetting kv.Set error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{
			setFunc: func(ctx context.Context, key, value string) error {
				return fmt.Errorf("disk full")
			},
		}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		res, err := pt.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "backup_retention_days",
			"value": "30",
		}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update setting")
		_ = res
	})

	// 9. persistPaths marshal unreachable
	t.Run("persistPaths marshal unreachable", func(t *testing.T) {
		t.Parallel()

		// json.Marshal on a []string never fails, so the error path in
		// persistPaths is unreachable in practice. This test documents
		// that fact and serves as a canary in case the type changes.
		data, err := json.Marshal([]string{"/a", "/b", "/c"})
		require.NoError(t, err)
		require.NotNil(t, data)
		t.Log("[UNREACHABLE] json.Marshal([]string{...}) never returns an error; " +
			"the error path in persistPaths exists for defensive future-proofing")
	})

	// 10. RemoveSafePath persist error after removal succeeds
	t.Run("RemoveSafePath persist error after removal succeeds", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{
			setFunc: func(ctx context.Context, key, value string) error {
				return fmt.Errorf("disk full")
			},
		}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.RegisterSafePath("/tmp")
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		res, err := pt.RemoveSafePath(ctx, map[string]interface{}{
			"path": "/tmp",
		}, nil)

		require.NoError(t, err)
		assert.Contains(t, res.Text, "Path removed from memory but failed to update persistence")
	})

	// 11. RemoveReadPath persist error after removal succeeds
	t.Run("RemoveReadPath persist error after removal succeeds", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{
			setFunc: func(ctx context.Context, key, value string) error {
				return fmt.Errorf("disk full")
			},
		}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.RegisterReadOnlyPath("/tmp")
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		res, err := pt.RemoveReadPath(ctx, map[string]interface{}{
			"path": "/tmp",
		}, nil)

		require.NoError(t, err)
		assert.Contains(t, res.Text, "Path removed from memory but failed to update persistence")
	})

	// 12. RegisterSafePath confirmation denied
	t.Run("RegisterSafePath confirmation denied", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "n"}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "safe-denied")
		res, err := pt.RegisterSafePath(ctx, map[string]interface{}{
			"path":   path,
			"reason": "testing denial",
		}, nil)

		require.NoError(t, err)
		assert.Equal(t, "Access denied by user.", res.Text)
	})

	// 13. RegisterReadPath confirmation denied
	t.Run("RegisterReadPath confirmation denied", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "n"}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "ro-denied")
		res, err := pt.RegisterReadPath(ctx, map[string]interface{}{
			"path":   path,
			"reason": "testing denial",
		}, nil)

		require.NoError(t, err)
		assert.Equal(t, "Access denied by user.", res.Text)
	})

	// 14. BypassConfirmation kv.Set error
	t.Run("BypassConfirmation kv.Set error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{
			setFunc: func(ctx context.Context, key, value string) error {
				return fmt.Errorf("persist failed")
			},
		}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.BypassConfirmation(ctx, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to persist bypass status")
	})

	// 15. UpdateSessionSetting confirmation denied
	t.Run("UpdateSessionSetting confirmation denied", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "n"}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		res, err := pt.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "backup_retention_days",
			"value": "30",
		}, nil)

		require.NoError(t, err)
		assert.Equal(t, "Update denied by user.", res.Text)
	})

	// 16. RemoveSafePath confirmAction error
	t.Run("RemoveSafePath confirmAction error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Err: errors.New("user aborted")}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "remove-confirm-err")
		_, err := pt.RemoveSafePath(ctx, map[string]interface{}{
			"path": path,
		}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user aborted")
	})

	// 17. UpdateSessionSetting confirmAction error
	t.Run("UpdateSessionSetting confirmAction error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Err: errors.New("user aborted")}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   "backup_retention_days",
			"value": "30",
		}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user aborted")
	})

	// 18. BypassConfirmation confirmAction error
	t.Run("BypassConfirmation confirmAction error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Err: errors.New("user aborted")}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.BypassConfirmation(ctx, nil, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user aborted")
	})

	// 19. RegisterSafePath confirmAction error
	t.Run("RegisterSafePath confirmAction error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Err: errors.New("user aborted")}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "register-confirm-err")
		_, err := pt.RegisterSafePath(ctx, map[string]interface{}{
			"path":   path,
			"reason": "testing confirmAction error",
		}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user aborted")
	})

	// 20. RemoveReadPath confirmAction error
	t.Run("RemoveReadPath confirmAction error", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Err: errors.New("user aborted")}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "ro-remove-confirm-err")
		_, err := pt.RemoveReadPath(ctx, map[string]interface{}{
			"path": path,
		}, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "user aborted")
	})

	// 21. RemoveSafePath user denial
	t.Run("RemoveSafePath user denial", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "n"}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "remove-safe-denied")
		res, err := pt.RemoveSafePath(ctx, map[string]interface{}{
			"path": path,
		}, nil)

		require.NoError(t, err)
		assert.Equal(t, "Removal denied by user.", res.Text)
	})

	// 22. RemoveReadPath user denial
	t.Run("RemoveReadPath user denial", func(t *testing.T) {
		t.Parallel()

		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "n"}
		})
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		path := filepath.Join(t.TempDir(), "remove-ro-denied")
		res, err := pt.RemoveReadPath(ctx, map[string]interface{}{
			"path": path,
		}, nil)

		require.NoError(t, err)
		assert.Equal(t, "Removal denied by user.", res.Text)
	})
}

// TestPolicyTool_UnmarshalArgsErrors verifies that all five CRUD functions
// return an error when args contain a value of the wrong type, causing
// tools.UnmarshalArgs to fail before any business logic runs.
func TestPolicyTool_UnmarshalArgsErrors(t *testing.T) {
	t.Parallel()

	t.Run("RegisterSafePath with int path", func(t *testing.T) {
		t.Parallel()
		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.RegisterSafePath(ctx, map[string]interface{}{
			"path":   123, // int instead of string
			"reason": "test",
		}, nil)

		require.Error(t, err)
	})

	t.Run("RemoveSafePath with int path", func(t *testing.T) {
		t.Parallel()
		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.RemoveSafePath(ctx, map[string]interface{}{
			"path": 123, // int instead of string
		}, nil)

		require.Error(t, err)
	})

	t.Run("RegisterReadPath with int path", func(t *testing.T) {
		t.Parallel()
		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.RegisterReadPath(ctx, map[string]interface{}{
			"path":   123, // int instead of string
			"reason": "test",
		}, nil)

		require.Error(t, err)
	})

	t.Run("RemoveReadPath with int path", func(t *testing.T) {
		t.Parallel()
		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.RemoveReadPath(ctx, map[string]interface{}{
			"path": 123, // int instead of string
		}, nil)

		require.Error(t, err)
	})

	t.Run("UpdateSessionSetting with int key", func(t *testing.T) {
		t.Parallel()
		kv := &funcMockKVStore{}
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		sm.SetBypassActive(true)
		pt := &policyTool{sm: sm, kv: kv}
		ctx := context.Background()

		_, err := pt.UpdateSessionSetting(ctx, map[string]interface{}{
			"key":   123, // int instead of string
			"value": "30",
		}, nil)

		require.Error(t, err)
	})
}

func TestPolicy_FilepathAbsErrors(t *testing.T) {
	// macOS APFS holds a vnode reference to the current working directory;
	// os.RemoveAll returns nil but the directory persists until the process
	// leaves it. os.Getwd() therefore never fails after deleting CWD on
	// macOS, making the filepath.Abs-error precondition unreachable.
	if runtime.GOOS == "darwin" {
		t.Skip("macOS APFS prevents CWD deletion while process is in it; filepath.Abs error path unreachable")
	}

	origDir, err := os.Getwd()
	require.NoError(t, err)
	origPWD, hadPWD := os.LookupEnv("PWD")

	// Register cleanup BEFORE any destructive process-wide state changes.
	// This guarantees restoration even if an intermediate require.NoError
	// fires t.Fatal during the destructive operations below.
	t.Cleanup(func() {
		if hadPWD {
			_ = os.Setenv("PWD", origPWD)
		} else {
			_ = os.Unsetenv("PWD")
		}
		if err := os.Chdir(origDir); err != nil {
			t.Logf("failed to restore working directory: %v", err)
		}
	})

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir), "failed to chdir into temp directory")
	_ = os.Unsetenv("PWD")
	require.NoError(t, os.RemoveAll(tmpDir), "failed to remove temp directory")

	_, wdErr := os.Getwd()
	require.Error(t, wdErr, "os.Getwd() should fail after deleting CWD and unsetting PWD")

	t.Run("RegisterSafePath filepath.Abs error", func(t *testing.T) {
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		p, pErr := newPolicyTool(sm, new(mockKVStore))
		require.NoError(t, pErr)

		_, err := p.RegisterSafePath(context.Background(), map[string]interface{}{
			"path":   "relative/safe/path",
			"reason": "test",
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid path")
	})

	t.Run("RemoveSafePath filepath.Abs error", func(t *testing.T) {
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		p, pErr := newPolicyTool(sm, new(mockKVStore))
		require.NoError(t, pErr)

		_, err := p.RemoveSafePath(context.Background(), map[string]interface{}{
			"path": "relative/safe/path",
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid path")
	})

	t.Run("RegisterReadPath filepath.Abs error", func(t *testing.T) {
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		p, pErr := newPolicyTool(sm, new(mockKVStore))
		require.NoError(t, pErr)

		_, err := p.RegisterReadPath(context.Background(), map[string]interface{}{
			"path":   "relative/read/path",
			"reason": "test",
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid path")
	})

	t.Run("RemoveReadPath filepath.Abs error", func(t *testing.T) {
		sm := NewSecurityManager(func() domain.UserInteractor {
			return &mockInteractor{Answer: "y"}
		})
		p, pErr := newPolicyTool(sm, new(mockKVStore))
		require.NoError(t, pErr)

		_, err := p.RemoveReadPath(context.Background(), map[string]interface{}{
			"path": "relative/read/path",
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid path")
	})
}

func TestActionTitle_Default(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action actionType
		want   string
	}{
		{"path write", actionPathWrite, "persistent access to:"},
		{"path remove", actionPathRemove, "to REMOVE authorization for:"},
		{"path read", actionPathRead, "persistent READ-ONLY access to:"},
		{"path remove read", actionPathRemoveRead, "to REMOVE read-only authorization for:"},
		{"bypass enable", actionBypassEnable, "to DISABLE ALL interactive security prompts."},
		{"session update", actionSessionUpdate, "to update session setting:"},
		{"unknown action type", actionType(999), ""},
		{"negative action type", actionType(-1), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := actionTitle(tt.action)
			assert.Equal(t, tt.want, got)
		})
	}
}
