// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
)

func TestTerminalMutexConcurrency(t *testing.T) {
	// Backup original stderr
	oldStderr := os.Stderr
	defer func() { os.Stderr = oldStderr }()

	// Create a pipe to capture output
	r, w, _ := os.Pipe()
	os.Stderr = w

	const (
		numGoroutines = 50
		iterations    = 100
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Stress test the mutex by having many goroutines write to stderr
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				TerminalMutex.Lock()
				fmt.Fprintf(os.Stderr, "[ID:%d][Iter:%d]\n", id, j)
				TerminalMutex.Unlock()
			}
		}(i)
	}

	// Wait for completion in a separate goroutine to close the pipe
	go func() {
		wg.Wait()
		w.Close()
	}()

	// Read and verify output
	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	// Basic check: Ensure we have the expected number of lines
	lines := 0
	for _, char := range output {
		if char == '\n' {
			lines++
		}
	}

	expectedLines := numGoroutines * iterations
	if lines != expectedLines {
		t.Errorf("Expected %d lines of output, got %d", expectedLines, lines)
	}
}
