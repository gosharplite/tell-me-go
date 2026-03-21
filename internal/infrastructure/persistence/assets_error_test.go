// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"errors"
	"os"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/persistence"
)

// mockAssetFS implements domain.FileSystem for testing AssetStore error paths.
type mockAssetFS struct {
	domain.FileSystem
	MkdirAllFunc  func(ctx context.Context, path string, perm os.FileMode) error
	WriteFileFunc func(ctx context.Context, name string, data []byte, perm os.FileMode) error
	StatFunc      func(ctx context.Context, name string) (os.FileInfo, error)
}

func (m *mockAssetFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if m.MkdirAllFunc != nil {
		return m.MkdirAllFunc(ctx, path, perm)
	}
	return nil
}

func (m *mockAssetFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	if m.WriteFileFunc != nil {
		return m.WriteFileFunc(ctx, name, data, perm)
	}
	return nil
}

func (m *mockAssetFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if m.StatFunc != nil {
		return m.StatFunc(ctx, name)
	}
	return nil, os.ErrNotExist
}

func TestAssetStore_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	data := []byte("asset-data")

	tests := []struct {
		name      string
		setupMock func() *mockAssetFS
		wantErr   bool
		errString string
	}{
		{
			name: "MkdirAll fails",
			setupMock: func() *mockAssetFS {
				return &mockAssetFS{
					MkdirAllFunc: func(ctx context.Context, path string, perm os.FileMode) error {
						return errors.New("mkdir failed")
					},
				}
			},
			wantErr:   true,
			errString: "mkdir failed",
		},
		{
			name: "WriteFile fails",
			setupMock: func() *mockAssetFS {
				return &mockAssetFS{
					WriteFileFunc: func(ctx context.Context, name string, data []byte, perm os.FileMode) error {
						return errors.New("write failed")
					},
				}
			},
			wantErr:   true,
			errString: "write failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupMock()
			store := NewAssetStore(m, "/tmp/assets")

			_, err := store.Put(ctx, data)

			if (err != nil) != tt.wantErr {
				t.Fatalf("AssetStore.Put() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errString != "" {
				if err.Error() != tt.errString {
					t.Errorf("AssetStore.Put() error = %q, want %q", err.Error(), tt.errString)
				}
			}
		})
	}
}
