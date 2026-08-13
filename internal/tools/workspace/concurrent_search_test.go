// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

type searchMockFile struct {
	*bytes.Reader
	name string
}

func (f *searchMockFile) Close() error                      { return nil }
func (f *searchMockFile) Write(p []byte) (n int, err error) { return 0, io.EOF }
func (f *searchMockFile) Sync() error                       { return nil }
func (f *searchMockFile) ReadDir(n int) ([]os.DirEntry, error) {
	return nil, io.EOF
}
func (f *searchMockFile) Seek(offset int64, whence int) (int64, error) {
	return f.Reader.Seek(offset, whence)
}

// searchMockFileReadErr wraps searchMockFile but returns an error on Read
// after a specified number of successful reads. This exercises the scanFile
// → checkBinary → scanLines → scanner.Err() error path.
type searchMockFileReadErr struct {
	*searchMockFile
	readErr   error
	readCount int
	failAfter int
}

func (f *searchMockFileReadErr) Read(p []byte) (int, error) {
	if f.readCount >= f.failAfter {
		return 0, f.readErr
	}
	f.readCount++
	return f.Reader.Read(p)
}

type searchMockFS struct {
	persistence.FileSystem
	files        map[string][]byte
	walkErr      error
	openErrs     map[string]error
	openReadErrs map[string]error // path → error returned by Read after checkBinary
}

func (m *searchMockFS) Open(ctx context.Context, name string) (persistence.File, error) {
	if err, ok := m.openErrs[name]; ok {
		return nil, err
	}
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	f := &searchMockFile{Reader: bytes.NewReader(content), name: name}
	if readErr, ok := m.openReadErrs[name]; ok {
		return &searchMockFileReadErr{
			searchMockFile: f,
			readErr:        readErr,
			failAfter:      1, // fail after checkBinary's first read
		}, nil
	}
	return f, nil
}

func (m *searchMockFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &searchMockFileInfo{name: name, size: int64(len(content))}, nil
}

func (m *searchMockFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	if m.walkErr != nil {
		return m.walkErr
	}
	for path, content := range m.files {
		info := &searchMockFileInfo{name: path, size: int64(len(content))}
		if err := fn(path, info, nil); err != nil {
			if err == os.ErrNotExist {
				continue
			}
			return err
		}
	}
	return nil
}

type searchMockFileInfo struct {
	os.FileInfo
	name string
	size int64
}

func (m *searchMockFileInfo) Name() string       { return m.name }
func (m *searchMockFileInfo) Size() int64        { return m.size }
func (m *searchMockFileInfo) IsDir() bool        { return false }
func (m *searchMockFileInfo) ModTime() time.Time { return time.Now() }
func (m *searchMockFileInfo) Mode() os.FileMode  { return 0644 }
func (m *searchMockFileInfo) Sys() interface{}   { return nil }

type mockSP struct{}

func (s *mockSP) IsPathSafe(path string) (string, error) { return path, nil }
func (s *mockSP) IsPathWritable(path string) (string, error) {
	return path, nil
}
func (s *mockSP) IsCommandAllowed(command string) bool { return true }
func (s *mockSP) IsBypassActive() bool                 { return false }

// TestWalkAndProcess_HeartbeatCancellation tests the shouldSkipEntry ctx.Err()
// cancellation path in walkAndProcess (utils.go:82-84). When the context is
// already cancelled before the walk begins, shouldSkipEntry aborts early on
// the first file.
//
// GAP ACCEPTED (utils.go:85-87): The walkHeartbeat error return inside
// walkAndProcess is structurally unreachable because shouldSkipEntry
// (called immediately before walkHeartbeat on the same goroutine) also
// checks ctx.Err(). Context cancellation between the two calls would
// require a race-condition-level expiry. walkHeartbeat itself is at
// 100% coverage via TestWalkHeartbeat. See issue #836.
//
// For explicit coverage of the walkHeartbeat error path using a toggle
// context, see TestWalkAndProcess_HeartbeatErrorPropagation in utils_test.go.
func TestWalkAndProcess_HeartbeatCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	fs := &searchMockFS{
		files: make(map[string][]byte),
	}
	// Add 50+ files to trigger walkHeartbeat (count%50==0 at count=50)
	for i := 0; i < 55; i++ {
		fs.files[fmt.Sprintf("file_%d.txt", i)] = []byte("content")
	}

	sp := &mockSP{}
	hb := make(chan struct{}, 1)

	processed := 0
	err := walkAndProcess(ctx, sp, fs, ".", hb, func(path string) error {
		processed++
		return nil
	}, infra_persistence.NewWorkspacePolicy())

	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// Should have processed fewer than 55 files (aborted early)
	if processed >= 55 {
		t.Errorf("expected early abort, but processed all %d files", processed)
	}
}
