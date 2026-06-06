// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"errors"
	"testing"
)

// --- IsPathSafe (4 subtests) ---

func TestMockSecurityManager_IsPathSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockSecurityManager
		path    string
		want    string
		wantErr string
	}{
		{
			name: "AllowAll",
			mock: &MockSecurityManager{AllowAll: true},
			path: "/any",
			want: "/any",
		},
		{
			name: "BypassActive",
			mock: &MockSecurityManager{BypassActive: true},
			path: "/any",
			want: "/any",
		},
		{
			name: "default",
			mock: &MockSecurityManager{},
			path: "/any",
			want: "/any",
		},
		{
			name: "with_func",
			mock: &MockSecurityManager{
				IsSafeFunc: func(path string) (string, error) {
					return "safe:" + path, errors.New("custom")
				},
			},
			path:    "/x",
			want:    "safe:/x",
			wantErr: "custom",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.mock.IsPathSafe(tt.path)

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("got error %v; want %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if got != tt.want {
				t.Errorf("got %q; want %q", got, tt.want)
			}
		})
	}
}

// --- IsPathWritable (3 subtests) ---

func TestMockSecurityManager_IsPathWritable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockSecurityManager
		path    string
		want    string
		wantErr string
	}{
		{
			name: "AllowAll",
			mock: &MockSecurityManager{AllowAll: true},
			path: "/any",
			want: "/any",
		},
		{
			name: "default",
			mock: &MockSecurityManager{},
			path: "/any",
			want: "/any",
		},
		{
			name: "with_func",
			mock: &MockSecurityManager{
				IsWritableFunc: func(path string) (string, error) {
					return "writable:" + path, errors.New("writable error")
				},
			},
			path:    "/x",
			want:    "writable:/x",
			wantErr: "writable error",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.mock.IsPathWritable(tt.path)

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("got error %v; want %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if got != tt.want {
				t.Errorf("got %q; want %q", got, tt.want)
			}
		})
	}
}

// --- Authorize (3 subtests) ---

func TestMockSecurityManager_Authorize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockSecurityManager
		wantOk  bool
		wantErr string
	}{
		{
			name:   "AllowAll",
			mock:   &MockSecurityManager{AllowAll: true},
			wantOk: true,
		},
		{
			name:   "no_bypass",
			mock:   &MockSecurityManager{},
			wantOk: false,
		},
		{
			name: "with_func",
			mock: &MockSecurityManager{
				AuthorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
					return true, errors.New("auth error")
				},
			},
			wantOk:  true,
			wantErr: "auth error",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok, err := tt.mock.Authorize(context.Background(), "label", "detail", "reason", true)

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("got error %v; want %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if ok != tt.wantOk {
				t.Errorf("got %v; want %v", ok, tt.wantOk)
			}
		})
	}
}

// --- IsCommandAllowed (4 subtests) ---

func TestMockSecurityManager_IsCommandAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mock   *MockSecurityManager
		cmd    string
		wantOk bool
	}{
		{
			name:   "AllowAll",
			mock:   &MockSecurityManager{AllowAll: true},
			cmd:    "rm",
			wantOk: true,
		},
		{
			name:   "BypassActive",
			mock:   &MockSecurityManager{BypassActive: true},
			cmd:    "rm",
			wantOk: true,
		},
		{
			name: "in_map",
			mock: &MockSecurityManager{
				AllowedCommands: map[string]bool{"ls": true},
			},
			cmd:    "ls",
			wantOk: true,
		},
		{
			name: "not_in_map",
			mock: &MockSecurityManager{
				AllowedCommands: map[string]bool{"ls": true},
			},
			cmd:    "rm",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok := tt.mock.IsCommandAllowed(tt.cmd)
			if ok != tt.wantOk {
				t.Errorf("got %v; want %v", ok, tt.wantOk)
			}
		})
	}
}

// --- BypassActive (2 subtests: getter + setter combined) ---

func TestMockSecurityManager_BypassActive(t *testing.T) {
	t.Parallel()

	t.Run("IsBypassActive", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{BypassActive: true}
		if !mock.IsBypassActive() {
			t.Error("expected true")
		}
	})

	t.Run("SetBypassActive", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{}
		mock.SetBypassActive(true)
		if !mock.IsBypassActive() {
			t.Error("expected true after SetBypassActive(true)")
		}
	})
}

// --- Confirm (3 subtests: default, interactor, func override) ---

func TestMockSecurityManager_Confirm(t *testing.T) {
	t.Parallel()

	t.Run("default", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{}
		ok, err := mock.Confirm(context.Background(), "msg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected true from default fallthrough")
		}
	})

	t.Run("with_interactor", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{
			Interactor: &MockInteractor{Answer: "y"},
		}
		ok, err := mock.Confirm(context.Background(), "msg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected true from interactor with Answer='y'")
		}
		if mock.ConfirmCallCount == 0 {
			t.Error("expected ConfirmCallCount > 0")
		}
		if mock.LastConfirmText != "msg" {
			t.Errorf("got LastConfirmText %q; want 'msg'", mock.LastConfirmText)
		}
	})

	t.Run("with_func", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{
			ConfirmFunc: func(ctx context.Context, message string) (bool, error) {
				return false, errors.New("confirm error")
			},
		}
		ok, err := mock.Confirm(context.Background(), "msg")
		if err == nil || err.Error() != "confirm error" {
			t.Errorf("got error %v; want 'confirm error'", err)
		}
		if ok {
			t.Error("expected false from custom ConfirmFunc")
		}
	})
}

// --- No-ops (LogAudit, Close, TerminalLock, TerminalUnlock, Prompt, Warn, RegisterSafePath) ---

func TestMockSecurityManager_Noops(t *testing.T) {
	t.Parallel()

	mock := &MockSecurityManager{}

	// Must not panic
	t.Run("LogAudit", func(t *testing.T) {
		t.Parallel()
		mock.LogAudit("test", "arg1", "arg2")
	})

	t.Run("Close", func(t *testing.T) {
		t.Parallel()
		if err := mock.Close(); err != nil {
			t.Errorf("Close() = %v; want nil", err)
		}
	})

	t.Run("TerminalLock", func(t *testing.T) {
		t.Parallel()
		mock.TerminalLock()
	})

	t.Run("TerminalUnlock", func(t *testing.T) {
		t.Parallel()
		mock.TerminalUnlock()
	})

	t.Run("Prompt", func(t *testing.T) {
		t.Parallel()
		mock.Prompt("test prompt")
	})

	t.Run("Warn", func(t *testing.T) {
		t.Parallel()
		mock.Warn("test warning")
	})

	t.Run("RegisterSafePath", func(t *testing.T) {
		t.Parallel()
		mock.RegisterSafePath("/tmp")
	})
}

// --- ReadLine (2 subtests: default + with interactor) ---

func TestMockSecurityManager_ReadLine(t *testing.T) {
	t.Parallel()

	t.Run("default", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{}
		line, err := mock.ReadLine(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if line != "" {
			t.Errorf("got %q; want empty string", line)
		}
	})

	t.Run("with_interactor", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityManager{
			Interactor: &MockInteractor{Answer: "user input"},
		}
		line, err := mock.ReadLine(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if line != "user input" {
			t.Errorf("got %q; want 'user input'", line)
		}
	})
}
