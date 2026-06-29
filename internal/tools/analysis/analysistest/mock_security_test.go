// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysistest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestMockSecurityProvider_IsPathSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockSecurityProvider
		path    string
		want    string // checked only when wantErr is empty
		wantErr string // exact error message
	}{
		{
			name:    "DenyAll",
			mock:    &MockSecurityProvider{DenyAll: true},
			path:    "/any/path",
			wantErr: "path not authorized",
		},
		{
			name: "AbsFunc_error",
			mock: &MockSecurityProvider{
				AbsFunc: func(string) (string, error) {
					return "", errors.New("abs failure")
				},
			},
			path:    "/any/path",
			wantErr: "abs failure",
		},
		{
			name: "out_of_bounds",
			mock: &MockSecurityProvider{
				TempDir: "/safe",
			},
			path:    "/unsafe/other/file.go",
			wantErr: "path out of bounds",
		},
		{
			name: "out_of_bounds_relative",
			mock: &MockSecurityProvider{
				TempDir: "/safe",
			},
			path:    "../outside",
			wantErr: "path out of bounds",
		},
		{
			name: "safe_inside_tempdir",
			mock: &MockSecurityProvider{
				TempDir: "/safe",
			},
			path: "/safe/sub/file.go",
			want: func() string {
				abs, _ := filepath.Abs("/safe/sub/file.go")
				return abs
			}(), // platform-dependent: Unix preserves "/safe/sub/file.go", Windows prepends volume
		},
		{
			name: "zero_value_happy_path",
			mock: &MockSecurityProvider{},
			path: "/tmp/test.go",
			want: func() string {
				abs, _ := filepath.Abs("/tmp/test.go")
				return abs
			}(), // platform-dependent: Unix preserves "/tmp/test.go", Windows prepends volume like "C:\\tmp\\test.go"
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.mock.IsPathSafe(tt.path)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("got error %q; want %q", err.Error(), tt.wantErr)
				}
				if got != "" {
					t.Errorf("got path %q; want empty string on error", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// On the zero_value happy path, filepath.Abs may resolve differently
			// per platform if the path doesn't exist. We check it's non-empty
			// and, when the want is an exact absolute path, we compare directly.
			if tt.want != "" && got != tt.want {
				t.Errorf("got %q; want %q", got, tt.want)
			}
			if got == "" {
				t.Error("got empty path; want non-empty absolute path")
			}
		})
	}
}

func TestMockSecurityProvider_IsPathSafe_SuccessIsAbsolute(t *testing.T) {
	// Verify that IsPathSafe returns the same result as filepath.Abs
	// when AbsFunc is nil (default behavior) for a relative path.
	t.Parallel()

	mock := &MockSecurityProvider{}
	got, err := mock.IsPathSafe("relative/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected, err := filepath.Abs("relative/path")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	if got != expected {
		t.Errorf("got %q; want %q (from filepath.Abs)", got, expected)
	}
}

func TestMockSecurityProvider_IsPathSafe_DenyAllTrumpsAll(t *testing.T) {
	// DenyAll=true must reject even when AbsFunc and TempDir would otherwise allow.
	t.Parallel()

	mock := &MockSecurityProvider{
		DenyAll: true,
		TempDir: "/safe",
		AbsFunc: func(s string) (string, error) {
			return "/safe/" + s, nil
		},
	}

	_, err := mock.IsPathSafe("file.go")
	if err == nil {
		t.Fatal("expected error from DenyAll")
	}
	if err.Error() != "path not authorized" {
		t.Errorf("got error %q; want %q", err.Error(), "path not authorized")
	}
}

func TestMockSecurityProvider_IsPathWritable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mock    *MockSecurityProvider
		path    string
		want    string
		wantErr string
	}{
		{
			name:    "DenyAll",
			mock:    &MockSecurityProvider{DenyAll: true},
			path:    "/any",
			wantErr: "path not authorized",
		},
		{
			name: "AbsFunc_error",
			mock: &MockSecurityProvider{
				AbsFunc: func(string) (string, error) {
					return "", errors.New("abs failure")
				},
			},
			path:    "/any",
			wantErr: "abs failure",
		},
		{
			name: "happy_path_inside_tempdir",
			mock: &MockSecurityProvider{
				TempDir: "/safe",
			},
			path: "/safe/file.go",
			want: func() string {
				abs, _ := filepath.Abs("/safe/file.go")
				return abs
			}(), // platform-dependent: Unix preserves "/safe/file.go", Windows prepends volume
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.mock.IsPathWritable(tt.path)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("got error %q; want %q", err.Error(), tt.wantErr)
				}
				if got != "" {
					t.Errorf("got path %q; want empty string on error", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q; want %q", got, tt.want)
			}
		})
	}
}

func TestMockSecurityProvider_ErrorReturningMethods(t *testing.T) {
	t.Parallel()

	t.Run("Confirm_zero_value", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityProvider{}
		ok, err := mock.Confirm(context.Background(), "msg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected true from zero-value Confirm")
		}
	})

	t.Run("Authorize_zero_value", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityProvider{}
		ok, err := mock.Authorize(context.Background(), "label", "detail", "reason", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected true from zero-value Authorize")
		}
	})

	t.Run("ReadLine_zero_value", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityProvider{}
		line, err := mock.ReadLine(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if line != "" {
			t.Errorf("got %q; want empty string", line)
		}
	})

	t.Run("Close_zero_value", func(t *testing.T) {
		t.Parallel()
		mock := &MockSecurityProvider{}
		if err := mock.Close(); err != nil {
			t.Errorf("Close() = %v; want nil", err)
		}
	})
}

func TestMockSecurityProvider_Noops(t *testing.T) {
	t.Parallel()

	mock := &MockSecurityProvider{}

	t.Run("TerminalLock", func(t *testing.T) {
		t.Parallel()
		mock.TerminalLock() // must not panic
	})

	t.Run("TerminalUnlock", func(t *testing.T) {
		t.Parallel()
		mock.TerminalUnlock() // must not panic
	})

	t.Run("IsBypassActive", func(t *testing.T) {
		t.Parallel()
		if mock.IsBypassActive() {
			t.Error("expected false from zero-value IsBypassActive")
		}
	})

	t.Run("IsCommandAllowed", func(t *testing.T) {
		t.Parallel()
		if !mock.IsCommandAllowed("any") {
			t.Error("expected true from zero-value IsCommandAllowed")
		}
	})

	t.Run("Prompt", func(t *testing.T) {
		t.Parallel()
		mock.Prompt("test prompt") // must not panic
	})

	t.Run("Warn", func(t *testing.T) {
		t.Parallel()
		mock.Warn("test warning") // must not panic
	})

	t.Run("LogAudit", func(t *testing.T) {
		t.Parallel()
		mock.LogAudit("test", "arg1", "arg2") // must not panic
	})
}
