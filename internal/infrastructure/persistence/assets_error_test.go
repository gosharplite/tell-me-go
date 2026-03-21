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
	failOn map[string]error
}

func (m *mockAssetFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	if err := m.failOn["MkdirAll"]; err != nil {
		return err
	}
	return nil
}

func (m *mockAssetFS) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	if err := m.failOn["WriteFile"]; err != nil {
		return err
	}
	return nil
}

func (m *mockAssetFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if err := m.failOn["Stat"]; err != nil {
		return nil, err
	}
	return nil, os.ErrNotExist
}

func TestAssetStore_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	data := []byte("asset-data")

	tests := []struct {
		name      string
		failOn    map[string]error
		wantErr   bool
		errString string
	}{
		{
			name: "MkdirAll fails",
			failOn: map[string]error{
				"MkdirAll": errors.New("mkdir failed"),
			},
			wantErr:   true,
			errString: "mkdir failed",
		},
		{
			name: "WriteFile fails",
			failOn: map[string]error{
				"WriteFile": errors.New("write failed"),
			},
			wantErr:   true,
			errString: "write failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockAssetFS{
				failOn: tt.failOn,
			}
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
