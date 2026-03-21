// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestOSFileSystem_RapidConcurrentSaves(t *testing.T) {
	// Use t.TempDir() to create a safe test directory.
	tmpDir := t.TempDir()
	fs := &OSFileSystem{}
	targetFile := filepath.Join(tmpDir, "stress_test.json")
	ctx := context.Background()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			payload := []byte(fmt.Sprintf(`{"id": %d, "data": "some random data for stress testing"}`, id))
			if err := AtomicWrite(ctx, fs, targetFile, payload, 0644); err != nil {
				errors <- fmt.Errorf("goroutine %d failed: %w", id, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent write error: %v", err)
	}

	// Final verification that the file exists and is valid
	if _, err := os.Stat(targetFile); os.IsNotExist(err) {
		t.Errorf("Target file %s does not exist after concurrent writes", targetFile)
	}
}

func TestAtomicWrite_EXDEV_Fallback(t *testing.T) {
	mockFS := newMockFS()
	targetPath := "/mnt/data/target.txt"
	tempPath := "/tmp/target.txt.123.tmp"
	data := []byte("cross-device-data")
	ctx := context.Background()

	// Mock CreateTemp to return a fixed name for simplicity in this test
	mockFS.CreateTempFunc = func(dir, pattern string) (File, error) {
		buf := new(bytes.Buffer)
		mockFS.mu.Lock()
		mockFS.files[tempPath] = buf
		mockFS.mu.Unlock()
		return &mockFile{
			name:   tempPath,
			data:   buf,
			failOn: make(map[string]error),
		}, nil
	}

	// Configure the mock's Rename function to return an explicit cross-device link error
	mockFS.failOn["Rename"] = &os.LinkError{
		Op:  "rename",
		Old: tempPath,
		New: targetPath,
		Err: syscall.EXDEV,
	}

	// Execute an atomic write
	err := AtomicWrite(ctx, mockFS, targetPath, data, 0644)

	// Assert that the function catches the EXDEV error,
	// successfully executes the io.Copy fallback mechanism,
	// and returns nil (success) to the caller without bubbling up the OS error.
	if err != nil {
		t.Fatalf("AtomicWrite failed on EXDEV fallback: %v", err)
	}

	// Verify the data was written to the target path
	got, err := mockFS.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read target file from mock FS: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Expected data %q, got %q", string(data), string(got))
	}

	// Verify temp file was removed
	if _, err := mockFS.ReadFile(tempPath); err == nil {
		t.Error("Temp file should have been removed after successful fallback copy")
	}
}
