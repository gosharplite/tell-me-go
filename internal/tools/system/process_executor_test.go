// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package system

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestRunPipeline(t *testing.T) {
	executor := NewProcessExecutor()
	ctx := context.Background()

	t.Run("successful pipeline", func(t *testing.T) {
		pipedParts := [][]string{
			{"echo", "hello world"},
			{"cat"},
		}
		config := ExecutionConfig{}
		result, err := executor.RunPipeline(ctx, pipedParts, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
		if !strings.Contains(result.Output, "hello world") {
			t.Errorf("expected output to contain 'hello world', got %q", result.Output)
		}
	})

	t.Run("pipeline with error in middle", func(t *testing.T) {
		// Non-existent command in the middle might fail during start or later
		// But let's use a command that exits with error
		pipedParts := [][]string{
			{"echo", "test"},
			{"grep", "nonexistent_pattern"},
			{"cat"},
		}
		config := ExecutionConfig{}
		result, err := executor.RunPipeline(ctx, pipedParts, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The exit code should be from the LAST command if it's the one we care about, 
		// but usually if any fails, we might want to know. 
		// The current implementation takes the exit code from the LAST command.
		// grep will return 1, but cat will return 0.
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0 (from cat), got %d", result.ExitCode)
		}
	})

	t.Run("pipeline where last command fails", func(t *testing.T) {
		pipedParts := [][]string{
			{"echo", "test"},
			{"ls", "/nonexistent_directory_for_test_12345"},
		}
		config := ExecutionConfig{}
		result, err := executor.RunPipeline(ctx, pipedParts, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ExitCode == 0 {
			t.Errorf("expected non-zero exit code, got 0")
		}
		if !strings.Contains(result.Output, "Errors:") {
			t.Logf("Output: %s", result.Output)
			// Depending on the OS, ls might write to stderr which should be captured
		}
	})

	t.Run("feedback capture", func(t *testing.T) {
		pipedParts := [][]string{
			{"echo", "line1"},
			{"cat"},
		}
		var feedback strings.Builder
		config := ExecutionConfig{
			Feedback: &feedback,
		}
		_, _ = executor.RunPipeline(ctx, pipedParts, config)
		if feedback.Len() == 0 {
			t.Error("expected feedback to be captured")
		}
	})

	t.Run("output file", func(t *testing.T) {
		tmpFile := "test_output.txt"
		defer os.Remove(tmpFile)

		pipedParts := [][]string{
			{"echo", "file content"},
			{"cat"},
		}
		config := ExecutionConfig{
			OutputFile: tmpFile,
		}
		_, err := executor.RunPipeline(ctx, pipedParts, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(tmpFile)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if !strings.Contains(string(content), "file content") {
			t.Errorf("expected file to contain 'file content', got %q", string(content))
		}
	})
}
