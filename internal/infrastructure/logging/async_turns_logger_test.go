// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsyncTurnsLogger_Formatting(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "turns.log")

	logger, err := NewAsyncTurnsLogger(logFile)
	require.NoError(t, err)

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	tests := []struct {
		name     string
		logFn    func()
		contains []string
	}{
		{
			name: "Turn Header - Minimal",
			logFn: func() {
				logger.LogTurnStatus(ctx, events.TurnStatus{
					Timestamp:        now,
					SessionTurns:     0,
					Tokens:           100,
					MaxHistoryTokens: 1000,
				})
			},
			contains: []string{
				"────────────────────────────────────────────────────────────────────────────────",
				"╭─⠿ Turn 1",
				"[12:00:00] Payload: ~100/1000 tokens",
			},
		},
		{
			name: "Turn Header - With Mode",
			logFn: func() {
				logger.LogTurnStatus(ctx, events.TurnStatus{
					Timestamp:        now,
					SessionTurns:     1,
					MaxHistoryTurns:  10,
					Tokens:           200,
					MaxHistoryTokens: 2000,
					Mode:             "coder",
				})
			},
			contains: []string{
				"╭─⠿ Turn 2/10 - coder",
				"[12:00:00] Payload: ~200/2000 tokens - coder",
			},
		},
		{
			name: "Post-Call Metrics",
			logFn: func() {
				logger.LogTurnStatus(ctx, events.TurnStatus{
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
						Model:          "gpt-4",
					},
				})
			},
			contains: []string{
				"[12:00:00] Payload: 1200/5000 tokens",
				"[12:00:00] [gpt-4] M: 400 H: 800 C: 300 Th: 100  ($0.0050) [8.00s (ΣT: 0.00s)]",
			},
		},
		{
			name: "Final Ready Summary",
			logFn: func() {
				logger.LogTurnStatus(ctx, events.TurnStatus{
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
				})
			},
			contains: []string{
				"╰─⠿ Ready ($0.0050 $0.0123 $1.2345 $5.6789 M: 1000 H: 2000 66.7% O: 500)",
			},
		},
		{
			name: "System Message",
			logFn: func() {
				logger.LogSystemMessage(ctx, "Operation failed", "error")
			},
			contains: []string{
				"[Error] Operation failed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.logFn()
		})
	}

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	logStr := string(content)

	for _, tt := range tests {
		for _, c := range tt.contains {
			assert.Contains(t, logStr, c, "Log missing expected content for test: %s", tt.name)
		}
	}
}

func TestAsyncTurnsLogger_New_Error(t *testing.T) {
	_, err := NewAsyncTurnsLogger("/non/existent/path/to/logfile.log")
	assert.Error(t, err)
}

func TestAsyncTurnsLogger_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "concurrency.log")

	logger, err := NewAsyncTurnsLogger(logFile)
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
				logger.LogSystemMessage(context.Background(), fmt.Sprintf("Goroutine %d msg %d", id, j), "info")
			}
		}(i)
	}

	close(start)
	wg.Wait()

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	assert.Equal(t, numGoroutines*msgsPerGoroutine, len(lines))
}

func TestAsyncTurnsLogger_CloseStress(t *testing.T) {
	// Tests the fix for the race condition/panic on close
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_stress.log")

	logger, err := NewAsyncTurnsLogger(logFile)
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Continuous logging
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				logger.LogSystemMessage(context.Background(), "spam", "info")
			}
		}
	}()

	// Goroutine 2: Close after a short delay
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		err := logger.Close()
		assert.NoError(t, err)
		close(stop)
	}()

	wg.Wait()

	// Verify that logging after close doesn't panic
	assert.NotPanics(t, func() {
		logger.LogSystemMessage(context.Background(), "after close", "info")
		logger.LogTurnStatus(context.Background(), events.TurnStatus{})
	})
}

func TestAsyncTurnsLogger_Close_Twice(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_twice.log")

	logger, err := NewAsyncTurnsLogger(logFile)
	require.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)
}
