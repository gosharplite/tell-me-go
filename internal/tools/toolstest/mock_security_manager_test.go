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

// --- IsPathWritable (1 subtest) ---

func TestMockSecurityManager_IsPathWritable(t *testing.T) {
	t.Parallel()

	mock := &MockSecurityManager{AllowAll: true}
	path, err := mock.IsPathWritable("/any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/any" {
		t.Errorf("got %q; want '/any'", path)
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

// --- IsCommandAllowed (3 subtests) ---

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

// --- Confirm (2 subtests: interactor + func override) ---

func TestMockSecurityManager_Confirm(t *testing.T) {
	t.Parallel()

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
		if !mock.ConfirmCalled {
			t.Error("expected ConfirmCalled to be true")
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
