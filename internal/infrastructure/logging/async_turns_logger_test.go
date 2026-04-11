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
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// mockClock is a simple mock clock for testing.
type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time                         { return m.now }
func (m *mockClock) Since(t time.Time) time.Duration        { return m.now.Sub(t) }
func (m *mockClock) Sleep(d time.Duration)                  {}
func (m *mockClock) After(d time.Duration) <-chan time.Time { return nil }
func (m *mockClock) NewTicker(d time.Duration) clock.Ticker { return nil }
func (m *mockClock) Jitter(base float64) float64            { return base }

func TestAsyncTurnsLogger_Log(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "turns.log")
	ctx := context.Background()

	tl, err := NewAsyncTurnsLogger(ctx, fs, logFile, slog.Default())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return tl.Listen(ctx)
	})

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tl.(*asyncTurnsLogger).clock = &mockClock{now: now}

	tl.HandleEvent(ctx, events.SystemMessageEvent{
		Message: "hello",
		Level:   "info",
	})
	tl.HandleEvent(ctx, events.TurnStatusEvent{
		Status: events.TurnStatus{
			Timestamp:    now,
			SessionTurns: 0,
		},
	})

	cancel() // Stop the background worker
	err = g.Wait()
	require.NoError(t, err)

	err = tl.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	output := string(content)
	assert.Contains(t, output, "[12:00:00] [Info] hello")
	assert.Contains(t, output, "╭─⠿ Turn 1")
}

func TestAsyncTurnsLogger_New_Error(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	ctx := context.Background()
	_, err := NewAsyncTurnsLogger(ctx, fs, "/non/existent/path/to/logfile.log", slog.Default())
	assert.Error(t, err)
}

func TestAsyncTurnsLogger_Concurrency(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "concurrency.log")
	ctx := context.Background()

	tl, err := NewAsyncTurnsLogger(ctx, fs, logFile, slog.Default())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return tl.Listen(ctx)
	})

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
				tl.HandleEvent(ctx, events.SystemMessageEvent{
					Message: fmt.Sprintf("Goroutine %d msg %d", id, j),
					Level:   "info",
				})
			}
		}(i)
	}

	close(start) // release all goroutines at once
	wg.Wait()

	cancel()
	_ = g.Wait()

	err = tl.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// Fixed non-deterministic assertion: sending exactly 100 messages into a channel with a buffer size of 100
	// should never result in dropped messages, regardless of how slow the worker is.
	assert.Len(t, lines, numGoroutines*msgsPerGoroutine, "Exactly 100 messages should be processed without dropping")
}

type errorSyncFile struct {
	infra_persistence.File
}

func (f *errorSyncFile) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (f *errorSyncFile) Sync() error {
	return errors.New("sync failed")
}

func (f *errorSyncFile) Close() error {
	return nil
}

type errorSyncFS struct {
	infra_persistence.FileSystem
}

func (fs *errorSyncFS) OpenFile(ctx context.Context, _ string, _ int, _ os.FileMode) (infra_persistence.File, error) {
	return &errorSyncFile{}, nil
}

func TestAsyncTurnsLogger_SyncError(t *testing.T) {
	fs := &errorSyncFS{}
	handler := &slogHandler{}
	logger := slog.New(handler)
	ctx := context.Background()

	tl, err := NewAsyncTurnsLogger(ctx, fs, "dummy", logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return tl.Listen(ctx)
	})

	tl.HandleEvent(ctx, events.SystemMessageEvent{
		Message: "test message",
		Level:   "info",
	})

	cancel()
	_ = g.Wait()

	err = tl.Close()
	require.NoError(t, err)

	handler.mu.Lock()
	defer handler.mu.Unlock()

	found := false
	for _, r := range handler.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "failed to sync turns log") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should have logged sync error warning")
}

func TestAsyncTurnsLogger_Close_Twice(t *testing.T) {
	fs := &infra_persistence.OSFileSystem{}
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_twice.log")
	ctx := context.Background()

	logger, err := NewAsyncTurnsLogger(ctx, fs, logFile, slog.Default())
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

func (fs *blockingFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	return fs.file, nil
}

func TestAsyncTurnsLogger_BufferFull(t *testing.T) {
	block := make(chan struct{})
	file := &blockingFile{block: block}
	fs := &blockingFS{file: file}

	handler := &slogHandler{}
	logger := slog.New(handler)
	ctx := context.Background()

	// Inject a logger with the custom handler
	tl, err := NewAsyncTurnsLogger(ctx, fs, "dummy", logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return tl.Listen(ctx)
	})

	// Send 1002 messages.
	// 1st message will block in the worker's Write call.
	// Next 1000 messages will fill the channel buffer (capacity 1000).
	// 1002nd message will trigger the "buffer full" warning because the channel is full.
	for i := 0; i < 1002; i++ {
		tl.HandleEvent(ctx, events.SystemMessageEvent{
			Message: fmt.Sprintf("msg %d", i),
			Level:   "info",
		})
	}

	// Unblock the worker so it can finish
	close(block)

	cancel()
	_ = g.Wait()

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

func (fs *errorWriteFS) OpenFile(ctx context.Context, _ string, _ int, _ os.FileMode) (infra_persistence.File, error) {
	return &errorWriteFile{}, nil
}

func TestAsyncTurnsLogger_WriteError(t *testing.T) {
	fs := &errorWriteFS{}
	handler := &slogHandler{}
	logger := slog.New(handler)
	ctx := context.Background()

	tl, err := NewAsyncTurnsLogger(ctx, fs, "dummy", logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return tl.Listen(ctx)
	})

	tl.HandleEvent(ctx, events.SystemMessageEvent{
		Message: "test message",
		Level:   "info",
	})

	cancel()
	_ = g.Wait()

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

func (fs *spyFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	return fs.file, nil
}

func TestAsyncTurnsLogger_CallsSync(t *testing.T) {
	file := &spyFile{}
	fs := &spyFS{file: file}
	ctx := context.Background()

	tl, err := NewAsyncTurnsLogger(ctx, fs, "dummy", slog.Default())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return tl.Listen(ctx)
	})

	tl.HandleEvent(ctx, events.SystemMessageEvent{
		Message: "test message",
		Level:   "info",
	})

	cancel()
	_ = g.Wait()

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
