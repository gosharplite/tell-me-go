// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	stressMaxCapture = 10240
	stressLineCount  = 500
)

func TestProcessExecutor_Stress(t *testing.T) {
	executor := NewProcessExecutor()
	// Use multiple workers to truly stress concurrent execution and output handling
	numWorkers := 3

	errs := runStressPool(t, executor, numWorkers)
	verifyStressResults(t, errs)
}

// runStressPool orchestrates the worker pool and collects results.
func runStressPool(t *testing.T, executor *ProcessExecutor, numWorkers int) []error {
	results := make(chan error, numWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runStressWorker(t, executor, id, results)
		}(i)
	}

	wg.Wait()
	close(results)

	var errs []error
	for err := range results {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// runStressWorker executes the stress test logic for a single worker.
func runStressWorker(t *testing.T, executor *ProcessExecutor, workerID int, results chan<- error) {
	outputFile := filepath.Join(t.TempDir(), fmt.Sprintf("stress_output_%d.txt", workerID))

	res, err := executeStressCommand(executor, outputFile)
	if err != nil {
		results <- fmt.Errorf("worker %d command failed: %w", workerID, err)
		return
	}

	if err := verifyResultIntegrity(res); err != nil {
		results <- fmt.Errorf("worker %d result invalid: %w", workerID, err)
		return
	}

	if err := verifyFileIntegrity(outputFile); err != nil {
		results <- fmt.Errorf("worker %d file invalid: %w", workerID, err)
		return
	}

	results <- nil
}

func executeStressCommand(executor *ProcessExecutor, outputFile string) (ExecutionResult, error) {
	config := ExecutionConfig{
		MaxCapture: stressMaxCapture,
		OutputFile: outputFile,
	}

	// Command that produces high-volume, interleaved output with multi-byte UTF-8
	cmdStr := fmt.Sprintf(`
(for i in $(seq 1 %d); do echo "STDOUT line $i - some unicode: 世界😀"; done) &
(for i in $(seq 1 %d); do echo "STDERR line $i - some unicode: 世😀界" >&2; done) &
wait
`, stressLineCount, stressLineCount)

	return executor.RunCommand(context.Background(), []string{"sh", "-c", cmdStr}, config)
}

func verifyResultIntegrity(res ExecutionResult) error {
	if !res.Truncated {
		return fmt.Errorf("expected result to be truncated")
	}

	if len(res.Output) > stressMaxCapture {
		return fmt.Errorf("output length %d exceeds MaxCapture %d", len(res.Output), stressMaxCapture)
	}

	if !utf8.ValidString(res.Output) {
		return fmt.Errorf("output contains invalid UTF-8 sequences")
	}

	// Verify that we have some content in the truncated output
	if !strings.Contains(res.Output, "STDOUT") && !strings.Contains(res.Output, "[stderr]") {
		return fmt.Errorf("truncated output missing both STDOUT and [stderr] content")
	}

	return nil
}

func verifyFileIntegrity(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat output file: %w", err)
	}

	if fileInfo.Size() <= int64(stressMaxCapture) {
		return fmt.Errorf("output file size %d should be greater than MaxCapture %d", fileInfo.Size(), stressMaxCapture)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read output file: %w", err)
	}

	return checkFileLines(string(content))
}

func checkFileLines(content string) error {
	if !strings.Contains(content, "STDOUT") {
		return fmt.Errorf("output file missing STDOUT content")
	}
	if !strings.Contains(content, "STDERR") {
		return fmt.Errorf("output file missing STDERR content")
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "STDOUT line") && !strings.HasPrefix(line, "STDERR line") {
			return fmt.Errorf("detected corrupted line in output file: %q", line)
		}
	}
	return nil
}

// verifyStressResults checks that no unexpected errors occurred during stress testing.
func verifyStressResults(t *testing.T, errs []error) {
	for _, err := range errs {
		if err != nil {
			t.Errorf("stress verification failed: %v", err)
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
