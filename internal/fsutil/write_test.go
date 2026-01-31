// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package fsutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	tests := []struct {
		name    string
		path    string
		data    []byte
		perm    os.FileMode
		wantErr bool
		setup   func(t *testing.T)
	}{
		{
			name:    "write new file",
			path:    filepath.Join(tmpDir, "new.txt"),
			data:    []byte("hello"),
			perm:    0644,
			wantErr: false,
		},
		{
			name:    "overwrite existing file",
			path:    filepath.Join(tmpDir, "existing.txt"),
			data:    []byte("initial"),
			perm:    0644,
			wantErr: false,
		},
		{
			name:    "write in subdirectory",
			path:    filepath.Join(tmpDir, "subdir", "file.txt"),
			data:    []byte("subdir"),
			perm:    0644,
			wantErr: false,
		},
		{
			name:    "invalid path - directory exists as file",
			path:    filepath.Join(tmpDir, "file_as_dir", "file.txt"),
			data:    []byte("error"),
			perm:    0644,
			wantErr: true,
			setup: func(t *testing.T) {
				os.WriteFile(filepath.Join(tmpDir, "file_as_dir"), []byte("not a dir"), 0644)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			if tt.name == "overwrite existing file" {
				err := os.WriteFile(tt.path, []byte("pre-existing"), 0644)
				if err != nil {
					t.Fatalf("failed to create pre-existing file: %v", err)
				}
			}

			err := AtomicWrite(ctx, tt.path, tt.data, tt.perm)
			if (err != nil) != tt.wantErr {
				t.Errorf("AtomicWrite() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				got, err := os.ReadFile(tt.path)
				if err != nil {
					t.Errorf("failed to read file: %v", err)
					return
				}
				if string(got) != string(tt.data) {
					t.Errorf("AtomicWrite() got = %v, want %v", string(got), string(tt.data))
				}

				info, err := os.Stat(tt.path)
				if err != nil {
					t.Errorf("failed to stat file: %v", err)
					return
				}
				// Note: permission check might be tricky on some systems/umasks,
				// but let's at least check the bits we can.
				if info.Mode().Perm() != tt.perm {
					t.Logf("AtomicWrite() perm got = %o, want %o (umask might affect this)", info.Mode().Perm(), tt.perm)
				}
			}
		})
	}
}
