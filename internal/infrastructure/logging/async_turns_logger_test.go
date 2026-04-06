// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
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
	assert.Greater(t, len(lines), 0, "Should have written some lines")
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

func TestAsyncTurnsLogger_BufferFull(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "buffer_full.log")

	handler := &slogHandler{}
	logger := slog.New(handler)

	// Inject a logger with the custom handler
	tl, err := NewAsyncTurnsLogger(fs, logFile, logger)
	require.NoError(t, err)

	// Send > 100 messages quickly.
	// Since the worker is asynchronous, this should fill the channel buffer.
	for i := 0; i < 200; i++ {
		tl.LogString(fmt.Sprintf("msg %d", i))
	}

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
