// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestAtomicWrite_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	data := []byte("test-data")
	path := "/data/test.txt"

	tests := []struct {
		name       string
		setupMock  func() *mockFileSystem
		wantErr    bool
		errPattern string
	}{
		{
			name: "MkdirAll fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.MkdirAllFunc = func(path string, perm os.FileMode) error {
					return errors.New("disk full")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to create directory: disk full",
		},
		{
			name: "CreateTemp fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(dir, pattern string) (File, error) {
					return nil, errors.New("permission denied")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to create temp file: permission denied",
		},
		{
			name: "Sync fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						SyncFunc: func() error {
							return errors.New("I/O error during sync")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to sync temp file: I/O error during sync",
		},
		{
			name: "Chmod fails",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						ChmodFunc: func(mode os.FileMode) error {
							return errors.New("chmod not supported")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to chmod temp file: chmod not supported",
		},
		{
			name: "Close fails after sync",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.CreateTempFunc = func(dir, pattern string) (File, error) {
					return &mockFile{
						name: dir + "/temp123",
						data: new(bytes.Buffer),
						CloseFunc: func() error {
							return errors.New("close failed")
						},
					}, nil
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to close temp file: close failed",
		},
		{
			name: "Rename fails (not EXDEV)",
			setupMock: func() *mockFileSystem {
				m := newMockFS()
				m.RenameFunc = func(oldpath, newpath string) error {
					return errors.New("rename denied")
				}
				return m
			},
			wantErr:    true,
			errPattern: "failed to rename temp file: rename denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupMock()

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

// Test A: Simulated "Disk Full" (Mock Injection)
func TestAtomicWrite_DiskFull(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()

	m.CreateTempFunc = func(dir, pattern string) (File, error) {
		mf := &mockFile{
			name: dir + "/temp123",
			data: new(bytes.Buffer),
			WriteFunc: func(p []byte) (n int, err error) {
				return 0, syscall.ENOSPC
			},
		}
		return mf, nil
	}

	err := AtomicWrite(ctx, m, "/any/path", []byte("data"), 0644)
	if err == nil {
		t.Fatal("expected error for disk full (ENOSPC), got nil")
	}

	if !errors.Is(err, syscall.ENOSPC) && !strings.Contains(err.Error(), "no space left on device") {
		t.Errorf("expected ENOSPC error, got: %v", err)
	}
}

// Test B: Real OS Permission Denied (Integration Test)
func TestAtomicWrite_OSPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping OS permission test on Windows due to ACL flakiness")
	}

	tempDir := t.TempDir()
	// Create a subdirectory that we will make read-only
	targetDir := fmt.Sprintf("%s/readonly", tempDir)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	// Remove write permissions from the directory
	if err := os.Chmod(targetDir, 0555); err != nil {
		t.Fatalf("failed to chmod target dir: %v", err)
	}
	defer func() { _ = os.Chmod(targetDir, 0755) }() // Clean up permissions so TempDir can be removed

	fs := &OSFileSystem{}
	err := AtomicWrite(context.Background(), fs, targetDir+"/test.txt", []byte("data"), 0644)

	if err == nil {
		t.Fatal("expected error when writing to read-only directory, got nil")
	}

	if !errors.Is(err, os.ErrPermission) && !errors.Is(err, syscall.EACCES) {
		t.Errorf("expected permission denied error, got: %v", err)
	}
}

// Test C: EXDEV Fallback Success (Mock Injection)
func TestAtomicWrite_EXDEVFallback(t *testing.T) {
	ctx := context.Background()
	m := newMockFS()

	data := []byte("important data")
	targetPath := "/mnt/external/file.txt"

	// Configure Rename to return EXDEV
	m.RenameFunc = func(oldpath, newpath string) error {
		return syscall.EXDEV
	}

	// We need to make sure CreateTemp works and adds to files map for OpenFile to find it
	m.CreateTempFunc = func(dir, pattern string) (File, error) {
		name := dir + "/temp123"
		mf := &mockFile{
			name: name,
			data: new(bytes.Buffer),
		}
		m.mu.Lock()
		m.files[name] = mf.data
		m.mu.Unlock()
		return mf, nil
	}

	err := AtomicWrite(ctx, m, targetPath, data, 0644)
	if err != nil {
		t.Fatalf("expected success with EXDEV fallback, got error: %v", err)
	}

	// Verify the data was "copied" to the target path in the mock filesystem
	m.mu.Lock()
	savedData, ok := m.files[targetPath]
	m.mu.Unlock()

	if !ok {
		t.Fatal("target file does not exist in mock filesystem after fallback")
	}

	if !bytes.Equal(savedData.Bytes(), data) {
		t.Errorf("saved data mismatch: got %q, want %q", savedData.Bytes(), data)
	}
}

type fallbackTestCase struct {
	name          string
	setupMock     func() *mockFileSystem
	wantErr       bool
	errContains   string
	expectRemoved string
}

func setupMockOpenFileSourceFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(name string, flag int, perm os.FileMode) (File, error) {
		if name == "/src" {
			return nil, errors.New("open source failed")
		}
		return nil, os.ErrNotExist
	}
	return m
}

func setupMockOpenFileDestinationFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(name string, flag int, perm os.FileMode) (File, error) {
		if name == "/src" {
			return &mockFile{name: name, data: new(bytes.Buffer)}, nil
		}
		if name == "/dst" {
			return nil, errors.New("open destination failed")
		}
		return nil, os.ErrNotExist
	}
	return m
}

func setupMockIoCopyFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(name string, flag int, perm os.FileMode) (File, error) {
		data := new(bytes.Buffer)
		if name == "/src" {
			data.Write([]byte("some data"))
		}
		mf := &mockFile{
			name: name,
			data: data,
		}
		if name == "/dst" {
			mf.WriteFunc = func(p []byte) (n int, err error) {
				return 0, errors.New("copy failed")
			}
		}
		return mf, nil
	}
	return m
}

func setupMockSyncFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(name string, flag int, perm os.FileMode) (File, error) {
		mf := &mockFile{
			name: name,
			data: new(bytes.Buffer),
		}
		if name == "/dst" {
			mf.SyncFunc = func() error {
				return errors.New("sync failed")
			}
		}
		return mf, nil
	}
	return m
}

func setupMockCloseFails() *mockFileSystem {
	m := newMockFS()
	m.OpenFileFunc = func(name string, flag int, perm os.FileMode) (File, error) {
		mf := &mockFile{
			name: name,
			data: new(bytes.Buffer),
		}
		if name == "/src" {
			mf.data.Write([]byte("data"))
		}
		if name == "/dst" {
			mf.CloseFunc = func() error {
				return errors.New("close failed")
			}
		}
		return mf, nil
	}
	return m
}

func buildFallbackTestCases() []fallbackTestCase {
	return []fallbackTestCase{
		{
			name:        "OpenFile source fails",
			setupMock:   setupMockOpenFileSourceFails,
			wantErr:     true,
			errContains: "fallback: failed to open source",
		},
		{
			name:        "OpenFile destination fails",
			setupMock:   setupMockOpenFileDestinationFails,
			wantErr:     true,
			errContains: "fallback: failed to open destination",
		},
		{
			name:          "io.Copy fails",
			setupMock:     setupMockIoCopyFails,
			wantErr:       true,
			errContains:   "fallback: failed to copy data",
			expectRemoved: "/dst",
		},
		{
			name:          "Sync fails",
			setupMock:     setupMockSyncFails,
			wantErr:       true,
			errContains:   "fallback: failed to sync destination",
			expectRemoved: "/dst",
		},
		{
			name:        "Close fails on success",
			setupMock:   setupMockCloseFails,
			wantErr:     true,
			errContains: "close failed",
		},
	}
}

func TestFallbackCopy_Errors(t *testing.T) {
	cases := buildFallbackTestCases()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setupMock()

			err := fallbackCopy(m, "/src", "/dst", 0644)

			if (err != nil) != tt.wantErr {
				t.Fatalf("fallbackCopy() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("fallbackCopy() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
			}

			if tt.expectRemoved != "" {
				found := false
				for _, r := range m.removedFiles {
					if r == tt.expectRemoved {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %s to be removed, but it wasn't. Removed: %v", tt.expectRemoved, m.removedFiles)
				}
			}
		})
	}
}
