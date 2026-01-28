// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestTerminalMutexConcurrency(t *testing.T) {
	// Backup original stderr
	oldStderr := os.Stderr
	defer func() { os.Stderr = oldStderr }()

	// Create a pipe to capture output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := io.Copy(&buf, r); err != nil {
		t.Errorf("Failed to read from pipe: %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	expectedLines := numGoroutines * iterations
	if len(lines) != expectedLines {
		t.Errorf("Expected %d lines of output, got %d", expectedLines, len(lines))
	}

	// Verify line integrity using regex to ensure no interleaving/garbling
	re := regexp.MustCompile(`^\[ID:\d+\]\[Iter:\d+\]$`)
	for i, line := range lines {
		if !re.MatchString(line) {
			t.Errorf("Line %d is garbled or interleaved: %q", i+1, line)
		}
	}
}
