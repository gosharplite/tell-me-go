// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestProcessExecutor_Stress(t *testing.T) {
	executor := NewProcessExecutor()
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "stress_output.txt")

	// Ensure cleanup
	t.Cleanup(func() {
		os.Remove(outputFile)
	})

	// 10KB limit to give both streams a chance to write before truncation
	maxCapture := 10240
	config := ExecutionConfig{
		MaxCapture: maxCapture,
		OutputFile: outputFile,
	}

	// Command that produces high-volume, interleaved output with multi-byte UTF-8
	// We use two background loops to ensure true concurrency from the process side.
	lineCount := 500
	cmdStr := fmt.Sprintf(`
(for i in $(seq 1 %d); do echo "STDOUT line $i - some unicode: 世界😀"; done) &
(for i in $(seq 1 %d); do echo "STDERR line $i - some unicode: 世😀界" >&2; done) &
wait
`, lineCount, lineCount)

	res, err := executor.RunCommand(context.Background(), []string{"sh", "-c", cmdStr}, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	// Verify truncation logic
	if !res.Truncated {
		t.Error("expected result to be truncated, but it wasn't")
	}

	if len(res.Output) > maxCapture {
		t.Errorf("output length %d exceeds MaxCapture %d", len(res.Output), maxCapture)
	}

	// Verify UTF-8 validity
	if !utf8.ValidString(res.Output) {
		t.Error("output contains invalid UTF-8 sequences")
	}

	// Verify that we have some content in the truncated output
	if !strings.Contains(res.Output, "STDOUT") && !strings.Contains(res.Output, "[stderr]") {
		t.Error("truncated output missing both STDOUT and [stderr] content")
	}

	// Verify the output file contains MORE than the capture limit (it shouldn't be truncated)
	fileInfo, err := os.Stat(outputFile)
	if err != nil {
		t.Fatalf("failed to stat output file: %v", err)
	}

	if fileInfo.Size() <= int64(maxCapture) {
		t.Errorf("output file size %d should be greater than MaxCapture %d", fileInfo.Size(), maxCapture)
	}

	// Verify file content validity and concurrency
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	fileContent := string(content)
	if !strings.Contains(fileContent, "STDOUT") {
		t.Error("output file missing STDOUT content")
	}
	if !strings.Contains(fileContent, "STDERR") {
		t.Error("output file missing STDERR content")
	}

	fileLines := strings.Split(fileContent, "\n")
	for _, line := range fileLines {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "STDOUT line") && !strings.HasPrefix(line, "STDERR line") {
			t.Errorf("detected corrupted line in output file: %q", line)
		}
	}
}

func TestProcessExecutor_UTF8Boundary(t *testing.T) {
	executor := NewProcessExecutor()

	// "世界" is 6 bytes (3+3)
	// If we set MaxCapture to 4, it should only contain "世" (3 bytes)
	config := ExecutionConfig{
		MaxCapture: 4,
	}

	res, err := executor.RunCommand(context.Background(), []string{"echo", "世界"}, config)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}

	// echo adds a newline, so the output is "世界\n" (7 bytes)
	// "世" is 3 bytes. The next char is "界" (3 bytes).
	// If MaxCapture is 4, it takes "世" (3 bytes), then it can't take the first byte of "界".
	// So it should be "世".

	if res.Output != "世" {
		t.Errorf("expected output '世', got %q (len %d)", res.Output, len(res.Output))
	}
	if !res.Truncated {
		t.Error("expected Truncated to be true")
	}
	if !utf8.ValidString(res.Output) {
		t.Error("output is not valid UTF-8")
	}
}
