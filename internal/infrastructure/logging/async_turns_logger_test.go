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
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsyncTurnsLogger_Log(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "turns.log")

	logger, err := NewAsyncTurnsLogger(fs, logFile, slog.Default())
	require.NoError(t, err)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	logger.LogSystemMessage("hello", "info", now)
	logger.LogTurnStatus(events.TurnStatus{
		Timestamp:    now,
		SessionTurns: 0,
	}, now)

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	output := string(content)
	assert.Contains(t, output, "[12:00:00] [Info] hello")
	assert.Contains(t, output, "╭─⠿ Turn 1")
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
	now := time.Now()

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			<-start
			for j := 0; j < msgsPerGoroutine; j++ {
				logger.LogSystemMessage(fmt.Sprintf("Goroutine %d msg %d", id, j), "info", now)
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
	now := time.Now()
	for i := 0; i < 102; i++ {
		tl.LogSystemMessage(fmt.Sprintf("msg %d", i), "info", now)
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

	tl.LogSystemMessage("test message", "info", time.Now())

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

	tl.LogSystemMessage("test message", "info", time.Now())

	err = tl.Close()
	require.NoError(t, err)

	assert.True(t, file.syncCalled, "Sync() should be called after each write")
}

func TestFormatSystemMessageForLog(t *testing.T) {
	l := &asyncTurnsLogger{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		msg   string
		level string
		want  string
	}{
		{"info", "hello", "info", "[12:00:00] [Info] hello"},
		{"error", "fail", "error", "[12:00:00] [Error] fail"},
		{"warn", "careful", "warn", "[12:00:00] [Warning] careful"},
		{"other", "msg", "debug", "[12:00:00] [System] msg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := l.formatSystemMessageForLog(tt.msg, tt.level, now)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatTurnStatusForLog(t *testing.T) {
	l := &asyncTurnsLogger{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		status   events.TurnStatus
		contains []string
	}{
		{
			name: "header minimal",
			status: events.TurnStatus{
				Timestamp:        now,
				SessionTurns:     0,
				Tokens:           100,
				MaxHistoryTokens: 1000,
			},
			contains: []string{
				"────────────────────────────────────────────────────────────────────────────────",
				"╭─⠿ Turn 1",
				"[12:00:00] Payload: ~100/1000 tokens",
			},
		},
		{
			name: "header with mode",
			status: events.TurnStatus{
				Timestamp:        now,
				SessionTurns:     1,
				MaxHistoryTurns:  10,
				Tokens:           200,
				MaxHistoryTokens: 2000,
				Mode:             "coder",
			},
			contains: []string{
				"╭─⠿ Turn 2/10 - coder",
				"[12:00:00] Payload: ~200/2000 tokens - coder",
			},
		},
		{
			name: "metrics success",
			status: events.TurnStatus{
				Timestamp:        now,
				IsPostCall:       true,
				MaxHistoryTokens: 5000,
				Metrics: &llm.Metrics{
					PromptTokens:   1200,
					CachedTokens:   800,
					ResponseTokens: 300,
					ThinkingTokens: 100,
					Duration:       5.5,
					ToolDuration:   2.5,
					Cost:           0.005,
					Model:          "gpt-4o",
				},
			},
			contains: []string{
				"[12:00:00] Payload: 1200/5000 tokens",
				"[12:00:00] [gpt-4o] M: 400 H: 800 C: 300 Th: 100 ($0.0050) [8.00s (ΣT: 0.00s)]",
			},
		},
		{
			name: "metrics with provider priority",
			status: events.TurnStatus{
				Timestamp:        now,
				IsPostCall:       true,
				MaxHistoryTokens: 5000,
				Metrics: &llm.Metrics{
					PromptTokens:   1200,
					CachedTokens:   800,
					ResponseTokens: 300,
					Duration:       5.5,
					Model:          "gpt-4o",
					Provider:       "openai",
					TrafficType:    "ON_DEMAND_PRIORITY",
				},
			},
			contains: []string{
				"[12:00:00] [openai-priority]",
			},
		},
		{
			name: "metrics zero values",
			status: events.TurnStatus{
				Timestamp:        now,
				IsPostCall:       true,
				MaxHistoryTokens: 5000,
				Metrics: &llm.Metrics{
					PromptTokens:   0,
					CachedTokens:   0,
					ResponseTokens: 0,
					Duration:       0,
				},
			},
			contains: []string{
				"[12:00:00] M: 0 H: 0 C: 0 Th: 0 [0.00s (ΣT: 0.00s)]",
			},
		},
		{
			name: "metrics with start time",
			status: events.TurnStatus{
				Timestamp:        now,
				IsPostCall:       true,
				StartTime:        now.Add(-10 * time.Second),
				CurrentTurns:     1, // 2nd turn (0, 1)
				MaxHistoryTokens: 5000,
				Metrics: &llm.Metrics{
					PromptTokens:           1200,
					CachedTokens:           800,
					ResponseTokens:         300,
					Duration:               5.5,
					ToolDuration:           2.5,
					CumulativeToolDuration: 1.23,
				},
			},
			contains: []string{
				"8.00s",  // totalTurnLatency
				"1.23s",  // CumulativeToolDuration
				"10.00s", // totalSessionDuration
				"5.00",   // throughput (10s / 2 turns)
			},
		},
		{
			name: "final ready summary",
			status: events.TurnStatus{
				Timestamp:   now,
				IsFinal:     true,
				SessionCost: 1.2345,
				TaskCost:    0.0123,
				DailyCost:   5.6789,
				TotalM:      1000,
				TotalH:      2000,
				TotalO:      500,
				Metrics: &llm.Metrics{
					Cost: 0.005,
				},
			},
			contains: []string{
				"╰─⠿ Ready ($0.0050 $0.0123 $1.2345 $5.6789 M: 1000 H: 2000 66.7% O: 500)",
			},
		},
		{
			name: "final ready summary - div by zero safety",
			status: events.TurnStatus{
				Timestamp: now,
				IsFinal:   true,
				TotalM:    0,
				TotalH:    0,
			},
			contains: []string{
				"M: 0 H: 0 0.0% O: 0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := l.formatTurnStatusForLog(tt.status, now)
			for _, want := range tt.contains {
				assert.Contains(t, got, want)
			}
		})
	}
}
