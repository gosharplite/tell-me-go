// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsyncTurnsLogger_LogString(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "turns.log")

	logger, err := NewAsyncTurnsLogger(logFile)
	require.NoError(t, err)

	logger.LogString("hello")
	logger.LogString("world\n")

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	logStr := string(content)

	assert.Equal(t, "hello\nworld\n", logStr)
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
				logger.LogString(fmt.Sprintf("Goroutine %d msg %d", id, j))
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
				logger.LogString("spam")
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
		logger.LogString("after close")
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
