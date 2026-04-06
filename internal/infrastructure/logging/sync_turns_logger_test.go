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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncTurnsLogger_LogString(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "turns.log")

	logger, err := NewSyncTurnsLogger(logFile)
	require.NoError(t, err)

	logger.LogString("hello")
	logger.LogString("world\n")

	err = logger.Close()
	require.NoError(t, err)

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld\n", string(content))
}

func TestSyncTurnsLogger_New_Error(t *testing.T) {
	_, err := NewSyncTurnsLogger("/non/existent/path/to/logfile.log")
	assert.Error(t, err)
}

func TestSyncTurnsLogger_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "concurrency.log")

	logger, err := NewSyncTurnsLogger(logFile)
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

	assert.Equal(t, numGoroutines*msgsPerGoroutine, len(lines))
}

func TestSyncTurnsLogger_Close_Twice(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_twice.log")

	logger, err := NewSyncTurnsLogger(logFile)
	require.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)

	err = logger.Close()
	assert.NoError(t, err)
}
