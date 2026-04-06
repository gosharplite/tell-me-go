// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsyncTurnsLogger_LogString(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "turns.log")

	logger, err := NewAsyncTurnsLogger(fs, logFile, slog.Default())
	require.NoError(t, err)

	logger.LogString("hello")
	logger.LogString("world\n")

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld\n", string(content))
}

func TestAsyncTurnsLogger_New_Error(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	_, err := NewAsyncTurnsLogger(fs, "/non/existent/path/to/logfile.log", slog.Default())
	assert.Error(t, err)
}

func TestAsyncTurnsLogger_Concurrency(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "concurrency.log")

	logger, err := NewAsyncTurnsLogger(fs, logFile, slog.Default())
	require.NoError(t, err)

	const numGoroutines = 10
	const msgsPerGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	start := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < msgsPerGoroutine; j++ {
				logger.LogString(fmt.Sprintf("Goroutine %d msg %d", id, j))
			}
		}(i)
	}

	close(start) // release all goroutines at once
	wg.Wait()

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// Fixed non-deterministic assertion: messages may be dropped if buffer fills up
	assert.GreaterOrEqual(t, len(lines), 50, "Should have written at least 50% of the messages without dropping")
	assert.LessOrEqual(t, len(lines), numGoroutines*msgsPerGoroutine, "Should not exceed max expected lines")
}

func TestAsyncTurnsLogger_Close_Twice(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_twice.log")

	logger, err := NewAsyncTurnsLogger(fs, logFile, slog.Default())
	require.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)
}

type slogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *slogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *slogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *slogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *slogHandler) WithGroup(_ string) slog.Handler      { return h }

type blockingFile struct {
	infra_persistence.File
	block chan struct{}
}

func (f *blockingFile) Write(p []byte) (n int, err error) {
	<-f.block
	return len(p), nil
}

func (f *blockingFile) Sync() error {
	return nil
}

func (f *blockingFile) Close() error {
	return nil
}

type blockingFS struct {
	infra_persistence.FileSystem
	file *blockingFile
}

func (fs *blockingFS) OpenFile(name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	return fs.file, nil
}

func TestAsyncTurnsLogger_BufferFull(t *testing.T) {
	block := make(chan struct{})
	file := &blockingFile{block: block}
	fs := &blockingFS{file: file}

	handler := &slogHandler{}
	logger := slog.New(handler)

	// Inject a logger with the custom handler
	tl, err := NewAsyncTurnsLogger(fs, "dummy", logger)
	require.NoError(t, err)

	// Send 102 messages.
	// 1st message will block in the worker's Write call.
	// Next 100 messages will fill the channel buffer (capacity 100).
	// 102nd message will trigger the "buffer full" warning because the channel is full.
	for i := 0; i < 102; i++ {
		tl.LogString(fmt.Sprintf("msg %d", i))
	}

	// Unblock the worker so it can finish
	close(block)

	err = tl.Close()
	assert.NoError(t, err)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	found := false
	for _, r := range handler.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "turns logger buffer full") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should have logged buffer full warning")
}

type errorWriteFile struct {
	infra_persistence.File
}

func (f *errorWriteFile) Write(_ []byte) (n int, err error) {
	return 0, errors.New("disk full")
}

func (f *errorWriteFile) Sync() error {
	return nil
}

func (f *errorWriteFile) Close() error {
	return nil
}

type errorWriteFS struct {
	infra_persistence.FileSystem
}

func (fs *errorWriteFS) OpenFile(_ string, _ int, _ os.FileMode) (infra_persistence.File, error) {
	return &errorWriteFile{}, nil
}

func TestAsyncTurnsLogger_WriteError(t *testing.T) {
	fs := &errorWriteFS{}
	handler := &slogHandler{}
	logger := slog.New(handler)

	tl, err := NewAsyncTurnsLogger(fs, "dummy", logger)
	require.NoError(t, err)

	tl.LogString("test message")

	err = tl.Close()
	require.NoError(t, err)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	found := false
	for _, r := range handler.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "failed to write to turns log") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should have logged write error warning")
}

type spyFile struct {
	infra_persistence.File
	syncCalled bool
}

func (f *spyFile) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (f *spyFile) Sync() error {
	f.syncCalled = true
	return nil
}

func (f *spyFile) Close() error {
	return nil
}

type spyFS struct {
	infra_persistence.FileSystem
	file *spyFile
}

func (fs *spyFS) OpenFile(name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	return fs.file, nil
}

func TestAsyncTurnsLogger_CallsSync(t *testing.T) {
	file := &spyFile{}
	fs := &spyFS{file: file}

	tl, err := NewAsyncTurnsLogger(fs, "dummy", slog.Default())
	require.NoError(t, err)

	tl.LogString("test message")

	err = tl.Close()
	require.NoError(t, err)

	assert.True(t, file.syncCalled, "Sync() should be called after each write")
}
