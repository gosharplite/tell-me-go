// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestAtomicWrite_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	data := []byte("test-data")
	path := "/data/test.txt"

	tests := []struct {
		name       string
		setupMock  func(m *mockFileSystem)
		wantErr    bool
		errPattern string
	}{
		{
			name: "MkdirAll fails",
			setupMock: func(m *mockFileSystem) {
				m.failOn["MkdirAll"] = errors.New("disk full")
			},
			wantErr:    true,
			errPattern: "failed to create directory: disk full",
		},
		{
			name: "CreateTemp fails",
			setupMock: func(m *mockFileSystem) {
				m.failOn["CreateTemp"] = errors.New("permission denied")
			},
			wantErr:    true,
			errPattern: "failed to create temp file: permission denied",
		},
		{
			name: "Sync fails",
			setupMock: func(m *mockFileSystem) {
				m.failOn["Sync"] = errors.New("I/O error during sync")
			},
			wantErr:    true,
			errPattern: "failed to sync temp file: I/O error during sync",
		},
		{
			name: "Rename fails",
			setupMock: func(m *mockFileSystem) {
				m.failOn["Rename"] = errors.New("cross-device link")
			},
			wantErr:    true,
			errPattern: "failed to rename temp file: cross-device link",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMockFS()
			if tt.setupMock != nil {
				tt.setupMock(m)
			}

			// Special handling for my mock: inject sync/chmod errors into CreateTemp files
			m.CreateTempFunc = func(dir, pattern string) (File, error) {
				if err := m.failOn["CreateTemp"]; err != nil {
					return nil, err
				}
				mf := &mockFile{
					name:   dir + "/temp123",
					data:   new(bytes.Buffer),
					failOn: make(map[string]error),
				}
				if err := m.failOn["Sync"]; err != nil {
					mf.failOn["Sync"] = err
				}
				if err := m.failOn["Chmod"]; err != nil {
					mf.failOn["Chmod"] = err
				}
				return mf, nil
			}

			err := AtomicWrite(ctx, m, path, data, 0644)

			if (err != nil) != tt.wantErr {
				t.Fatalf("AtomicWrite() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errPattern != "" {
				if err.Error() != tt.errPattern {
					t.Errorf("AtomicWrite() error = %q, want %q", err.Error(), tt.errPattern)
				}
			}
		})
	}
}
