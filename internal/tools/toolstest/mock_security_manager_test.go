// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"errors"
	"testing"
)

func TestMockSecurityManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func() *MockSecurityManager
		check func(t *testing.T, m *MockSecurityManager)
	}{
		// --- IsPathSafe ---
		{
			name: "IsPathSafe_AllowAll",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{AllowAll: true}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				path, err := m.IsPathSafe("/any")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if path != "/any" {
					t.Errorf("got %q; want '/any'", path)
				}
			},
		},
		{
			name: "IsPathSafe_BypassActive",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{BypassActive: true}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				path, err := m.IsPathSafe("/any")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if path != "/any" {
					t.Errorf("got %q; want '/any'", path)
				}
			},
		},
		{
			name: "IsPathSafe_default",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				path, err := m.IsPathSafe("/any")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if path != "/any" {
					t.Errorf("got %q; want '/any'", path)
				}
			},
		},
		{
			name: "IsPathSafe_with_func",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{
					IsSafeFunc: func(path string) (string, error) {
						return "safe:" + path, errors.New("custom")
					},
				}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				path, err := m.IsPathSafe("/x")
				if err == nil || err.Error() != "custom" {
					t.Errorf("got error %v; want 'custom'", err)
				}
				if path != "safe:/x" {
					t.Errorf("got %q; want 'safe:/x'", path)
				}
			},
		},
		// --- IsPathWritable ---
		{
			name: "IsPathWritable_AllowAll",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{AllowAll: true}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				path, err := m.IsPathWritable("/any")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if path != "/any" {
					t.Errorf("got %q; want '/any'", path)
				}
			},
		},
		// --- Authorize ---
		{
			name: "Authorize_AllowAll",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{AllowAll: true}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				ok, err := m.Authorize(context.Background(), "label", "detail", "reason", true)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !ok {
					t.Error("expected true for AllowAll")
				}
			},
		},
		{
			name: "Authorize_no_bypass",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				ok, err := m.Authorize(context.Background(), "label", "detail", "reason", true)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if ok {
					t.Error("expected false when AllowAll and BypassActive are both false")
				}
			},
		},
		{
			name: "Authorize_with_func",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{
					AuthorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
						return true, errors.New("auth error")
					},
				}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				ok, err := m.Authorize(context.Background(), "label", "detail", "reason", true)
				if err == nil || err.Error() != "auth error" {
					t.Errorf("got error %v; want 'auth error'", err)
				}
				if !ok {
					t.Error("expected true from custom func")
				}
			},
		},
		// --- IsCommandAllowed ---
		{
			name: "IsCommandAllowed_AllowAll",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{AllowAll: true}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				if !m.IsCommandAllowed("rm") {
					t.Error("expected true for AllowAll")
				}
			},
		},
		{
			name: "IsCommandAllowed_in_map",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{
					AllowedCommands: map[string]bool{"ls": true},
				}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				if !m.IsCommandAllowed("ls") {
					t.Error("expected true for 'ls' in AllowedCommands")
				}
			},
		},
		{
			name: "IsCommandAllowed_not_in_map",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{
					AllowedCommands: map[string]bool{"ls": true},
				}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				if m.IsCommandAllowed("rm") {
					t.Error("expected false for 'rm' not in AllowedCommands")
				}
			},
		},
		// --- IsBypassActive / SetBypassActive ---
		{
			name: "IsBypassActive",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{BypassActive: true}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				if !m.IsBypassActive() {
					t.Error("expected true")
				}
			},
		},
		{
			name: "SetBypassActive",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				m.SetBypassActive(true)
				if !m.IsBypassActive() {
					t.Error("expected true after SetBypassActive(true)")
				}
			},
		},
		// --- Confirm ---
		{
			name: "Confirm_with_interactor",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{
					Interactor: &MockInteractor{Answer: "y"},
				}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				ok, err := m.Confirm(context.Background(), "msg")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !ok {
					t.Error("expected true from interactor with Answer='y'")
				}
				if !m.ConfirmCalled {
					t.Error("expected ConfirmCalled to be true")
				}
				if m.LastConfirmText != "msg" {
					t.Errorf("got LastConfirmText %q; want 'msg'", m.LastConfirmText)
				}
			},
		},
		{
			name: "Confirm_with_func",
			setup: func() *MockSecurityManager {
				return &MockSecurityManager{
					ConfirmFunc: func(ctx context.Context, message string) (bool, error) {
						return false, errors.New("confirm error")
					},
				}
			},
			check: func(t *testing.T, m *MockSecurityManager) {
				ok, err := m.Confirm(context.Background(), "msg")
				if err == nil || err.Error() != "confirm error" {
					t.Errorf("got error %v; want 'confirm error'", err)
				}
				if ok {
					t.Error("expected false from custom ConfirmFunc")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := tt.setup()
			tt.check(t, m)
		})
	}
}
