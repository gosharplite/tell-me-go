// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func assertPromptsMatch(t *testing.T, got, expected []string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Errorf("got %d prompts; want %d", len(got), len(expected))
	}
	for i, v := range got {
		if i < len(expected) && v != expected[i] {
			t.Errorf("at index %d: got %q; want %q", i, v, expected[i])
		}
	}
}

func TestGlobalPromptTracker(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()

	prompts := []string{"hello", "world", "hello", "foo", "bar", "hello"}
	for _, p := range prompts {
		if err := tracker.Append(context.Background(), p); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Verify LoadTopN (reverse order, no duplicates)
	// Last unique: hello, bar, foo, world
	got, err := tracker.LoadTopN(context.Background(), 5)
	if err != nil {
		t.Fatalf("LoadTopN failed: %v", err)
	}

	expected := []string{"hello", "bar", "foo", "world"}
	assertPromptsMatch(t, got, expected)

	// Test with smaller limit
	got2, err := tracker.LoadTopN(context.Background(), 2)
	if err != nil {
		t.Fatalf("LoadTopN(2) failed: %v", err)
	}
	if len(got2) != 2 {
		t.Errorf("got %d prompts; want 2", len(got2))
	}
	if got2[0] != "hello" || got2[1] != "bar" {
		t.Errorf("got %v; want [hello bar]", got2)
	}
}

func TestGlobalPromptTrackerNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(filepath.Join(tmpDir, "non-existent"))
	defer func() { _ = tracker.Close() }()

	got, err := tracker.LoadTopN(context.Background(), 10)
	if err != nil {
		t.Fatalf("LoadTopN from non-existent file failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d prompts; want 0", len(got))
	}
}

func TestGlobalPromptTracker_LargePayload_Over64KB(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()

	// Create a payload larger than 64KB (e.g., 70,000 chars)
	// We'll use a string that's clearly larger than bufio.MaxScanTokenSize (64*1024)
	largePrompt := "START_" + string(make([]byte, 70000)) + "_END"

	err := tracker.Append(context.Background(), largePrompt)
	if err != nil {
		t.Fatalf("Failed to append large prompt: %v", err)
	}

	// Attempt to load the prompt back
	got, err := tracker.LoadTopN(context.Background(), 1)
	if err != nil {
		t.Fatalf("LoadTopN failed for large payload: %v", err)
	}

	if len(got) == 0 {
		t.Fatal("LoadTopN returned 0 prompts, expected 1")
	}

	if got[0] != largePrompt {
		t.Errorf("Retrieved prompt length mismatch: got %d; want %d", len(got[0]), len(largePrompt))
		// Log a bit of the beginning/end for debugging
		if len(got[0]) > 20 && len(largePrompt) > 20 {
			t.Errorf("Start mismatch: got %q...; want %q...", got[0][:10], largePrompt[:10])
			t.Errorf("End mismatch: got ...%q; want ...%q", got[0][len(got[0])-10:], largePrompt[len(largePrompt)-10:])
		}
	}
}

func TestGlobalPromptTracker_LoadTopN_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()

	// Add 10 "duplicate" prompts
	for i := 0; i < 10; i++ {
		if err := tracker.Append(context.Background(), "duplicate"); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Add 5 unique prompts BEFORE the duplicates
	uniquePrompts := []string{"p1", "p2", "p3", "p4", "p5"}
	// We append them first, so they are at the beginning of the file.
	// But to test that it returns them even if they are far back:
	tracker2, _ := NewGlobalPromptTracker(t.TempDir()) // Reset
	defer func() { _ = tracker2.Close() }()
	for _, p := range uniquePrompts {
		if err := tracker2.Append(context.Background(), p); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := tracker2.Append(context.Background(), "duplicate"); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Should return ["duplicate", "p5", "p4", "p3", "p2"]
	limit := 5
	got, err := tracker2.LoadTopN(context.Background(), limit)
	if err != nil {
		t.Fatalf("LoadTopN failed: %v", err)
	}

	expected := []string{"duplicate", "p5", "p4", "p3", "p2"}
	assertPromptsMatch(t, got, expected)
}

func TestGlobalPromptTracker_Compaction(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	// Add duplicates and many entries
	for i := 0; i < 10; i++ {
		_ = tracker.Append(context.Background(), "duplicate")
	}
	_ = tracker.Append(context.Background(), "unique1")
	_ = tracker.Append(context.Background(), "unique2")
	_ = tracker.Append(context.Background(), "duplicate") // latest one

	// Manually trigger compaction
	tracker.compactLog(context.Background())

	// Verify file content after compaction
	// Should be: unique1, unique2, duplicate
	got, err := tracker.LoadTopN(context.Background(), 10)
	if err != nil {
		t.Fatalf("LoadTopN failed: %v", err)
	}

	expected := []string{"duplicate", "unique2", "unique1"}
	if len(got) != len(expected) {
		t.Errorf("got %d prompts; want %d", len(got), len(expected))
	}
	for i, v := range got {
		if v != expected[i] {
			t.Errorf("at index %d: got %q; want %q", i, v, expected[i])
		}
	}

	// Verify that the file actually shrunk (though with few entries it's hard to see,
	// but we can check the number of lines)
	content, _ := os.ReadFile(tracker.filepath)
	lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
	if len(lines) != 3 {
		t.Errorf("got %d lines; want 3", len(lines))
	}
}

func TestGlobalPromptTracker_AppendTriggersCompaction(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	done := make(chan struct{})
	tracker.testCompactionHook = func() {
		select {
		case <-done:
			// Already closed
		default:
			close(done)
		}
	}

	// Append a lot of duplicates to exceed 150KB
	largePrompt := string(bytes.Repeat([]byte("A"), 1000))
	for i := 0; i < 200; i++ {
		_ = tracker.Append(context.Background(), largePrompt)
	}

	// Wait for async compaction
	select {
	case <-done:
		// Success
	case <-time.After(3 * time.Second):
		t.Fatal("compaction deadlock or timeout")
	}

	content, err := os.ReadFile(tracker.filepath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
	finalLines := len(lines)

	if finalLines >= 200 {
		t.Errorf("expected compaction to reduce lines, got %d", finalLines)
	}
	if finalLines == 0 {
		t.Errorf("file should not be empty")
	}
}

func TestGlobalPromptTracker_Migration(t *testing.T) {
	t.Run("MigrateValidLegacyData", func(t *testing.T) {
		tmpDir := t.TempDir()
		relPath := filepath.Join(".tellmego", "prompts.jsonl")
		content := []byte(`{"timestamp":"2023-01-01T00:00:00Z","prompt":"legacy test"}` + "\n")

		t.Run("SetupLegacyData", func(t *testing.T) {
			setupLegacyFile(t, tmpDir, relPath, content)
		})

		t.Run("ExecuteMigration", func(t *testing.T) {
			runMigration(t, tmpDir)
		})

		t.Run("VerifyNewState", func(t *testing.T) {
			verifyMigration(t, tmpDir, relPath, content)
		})

		t.Run("CleanupLegacyFiles", func(t *testing.T) {
			// Legacy file removal checked in verifyMigration, check folder here
			if _, err := os.Stat(filepath.Join(tmpDir, ".tellmego")); !os.IsNotExist(err) {
				t.Errorf("expected .tellmego folder to be removed")
			}
		})
	})

	t.Run("MigrateFromRoot", func(t *testing.T) {
		tmpDir := t.TempDir()
		relPath := "global_prompts.jsonl"
		content := []byte(`{"timestamp":"2023-01-02T00:00:00Z","prompt":"root test"}` + "\n")

		setupLegacyFile(t, tmpDir, relPath, content)
		runMigration(t, tmpDir)
		verifyMigration(t, tmpDir, relPath, content)
	})

	t.Run("HandleCorruptLegacyData", func(t *testing.T) {
		tmpDir := t.TempDir()
		relPath := "global_prompts.jsonl"
		content := []byte("this is not json\nbut it should still be moved\n")

		setupLegacyFile(t, tmpDir, relPath, content)
		runMigration(t, tmpDir)
		verifyMigration(t, tmpDir, relPath, content)
	})

	t.Run("Idempotency", func(t *testing.T) {
		tmpDir := t.TempDir()
		relPath := "global_prompts.jsonl"
		content := []byte(`{"timestamp":"2023-01-03T00:00:00Z","prompt":"idempotency test"}` + "\n")

		setupLegacyFile(t, tmpDir, relPath, content)

		// Run migration twice
		runMigration(t, tmpDir)
		runMigration(t, tmpDir)

		verifyMigration(t, tmpDir, relPath, content)
	})

	t.Run("NoOverwrite", func(t *testing.T) {
		tmpDir := t.TempDir()
		legacyRel := "global_prompts.jsonl"
		legacyContent := []byte("legacy\n")
		setupLegacyFile(t, tmpDir, legacyRel, legacyContent)

		newRel := filepath.Join("output", "global_prompts.jsonl")
		newContent := []byte("new\n")
		// Use manual write for existing new file
		_ = os.MkdirAll(filepath.Join(tmpDir, "output"), 0755)
		_ = os.WriteFile(filepath.Join(tmpDir, newRel), newContent, 0644)

		runMigration(t, tmpDir)

		// Legacy file should STILL exist
		if _, err := os.Stat(filepath.Join(tmpDir, legacyRel)); os.IsNotExist(err) {
			t.Errorf("expected legacy file to still exist when new file is present")
		}

		// New file should NOT be overwritten
		got, _ := os.ReadFile(filepath.Join(tmpDir, newRel))
		if !bytes.Equal(got, newContent) {
			t.Errorf("new file was overwritten")
		}
	})
}

func setupLegacyFile(t *testing.T, homeDir, relPath string, content []byte) string {
	t.Helper()
	path := filepath.Join(homeDir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create directory for %s: %v", relPath, err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write legacy file %s: %v", relPath, err)
	}
	return path
}

func verifyMigration(t *testing.T, homeDir, legacyRelPath string, expectedContent []byte) {
	t.Helper()
	legacyFile := filepath.Join(homeDir, legacyRelPath)
	newFile := filepath.Join(homeDir, "output", "global_prompts.jsonl")

	// Verify legacy file is gone (if it was supposed to be migrated)
	if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
		t.Errorf("expected legacy file %s to be removed, but it still exists", legacyRelPath)
	}

	// Verify new file exists and content matches
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("failed to read migrated file: %v", err)
	}

	if !bytes.Equal(content, expectedContent) {
		t.Errorf("migrated content mismatch for %s", legacyRelPath)
	}
}

func runMigration(t *testing.T, homeDir string) {
	t.Helper()
	_, err := NewGlobalPromptTracker(homeDir)
	if err != nil {
		t.Fatalf("NewGlobalPromptTracker failed: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	// Use t.TempDir() for automatic cleanup and cross-platform compatibility
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	expectedContent := "test content for fallback migration"
	err := os.WriteFile(src, []byte(expectedContent), 0644)
	if err != nil {
		t.Fatalf("failed to create src: %v", err)
	}

	// Execute the utility directly
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// Verify data integrity
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read dst: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("expected %q, got %q", expectedContent, string(content))
	}

	// Test non-existent source
	err = copyFile(filepath.Join(tmpDir, "non-existent"), filepath.Join(tmpDir, "out"))
	if err == nil {
		t.Error("expected error for non-existent source, got nil")
	}

	// Test invalid destination
	err = copyFile(src, filepath.Join(tmpDir, "non-existent-dir", "out"))
	if err == nil {
		t.Error("expected error for invalid destination, got nil")
	}
}

func TestNewNoOpTracker(t *testing.T) {
	tracker := NewNoOpTracker()
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}

	err := tracker.Append(context.Background(), "test")
	if err != nil {
		t.Errorf("Append failed: %v", err)
	}

	got, err := tracker.LoadTopN(context.Background(), 10)
	if err != nil {
		t.Errorf("LoadTopN failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d prompts; want 0", len(got))
	}
}

func TestNewGlobalPromptTracker_MkdirError(t *testing.T) {
	// Create a file where a directory should be
	tmpDir := t.TempDir()
	conflictFile := filepath.Join(tmpDir, "output")
	if err := os.WriteFile(conflictFile, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewGlobalPromptTracker(tmpDir)
	if err == nil {
		t.Fatal("expected error when MkdirAll fails, got nil")
	}
	if tracker != nil {
		t.Error("expected nil tracker when initialization fails")
	}
}

func TestGlobalPromptTracker_CompactionIgnoresContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	// Set a small threshold for testing to trigger compaction quickly
	// We'll use a large prompt to trigger it
	largePrompt := string(bytes.Repeat([]byte("B"), 1000))

	// Prepare synchronization for the hook
	compactionDone := make(chan struct{})
	tracker.testCompactionHook = func() {
		close(compactionDone)
	}

	// Create a context and cancel it immediately after triggering Append
	ctx, cancel := context.WithCancel(context.Background())

	// We need enough entries to trigger the size threshold (> 150KB)
	for i := 0; i < 160; i++ {
		_ = tracker.Append(ctx, largePrompt)
	}

	// Cancel the context that was used for Append
	cancel()

	// Wait for compaction to finish
	select {
	case <-compactionDone:
		// Compaction finished successfully despite context cancellation
	case <-time.After(5 * time.Second):
		t.Fatal("Compaction timed out or was aborted by context cancellation")
	}

	// Final verification: file should exist and have unique content
	content, err := tracker.LoadTopN(context.Background(), 1)
	if err != nil {
		t.Fatalf("Failed to load after compaction: %v", err)
	}
	if len(content) == 0 || content[0] != largePrompt {
		t.Errorf("Data loss or corruption detected after compaction")
	}
}

type errorWriter struct{}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	return 0, io.ErrShortWrite
}

func TestPromptTracker_CompactionWriteFailure(t *testing.T) {
	tracker := &globalPromptTracker{}
	entries := []promptEntry{
		{Timestamp: "now", Prompt: "test"},
	}
	success := tracker.writeCompactedTempFile(&errorWriter{}, entries)
	if success {
		t.Errorf("Expected writeCompactedTempFile to fail with errorWriter")
	}
}

func TestGlobalPromptTracker_LoadTopN_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()

	_ = tracker.Append(context.Background(), "p1")
	_ = tracker.Append(context.Background(), "p2")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tracker.LoadTopN(ctx, 10)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestGlobalPromptTracker_LoadTopN_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	// Manually write malformed JSON lines
	malformedContent := `{"timestamp":"2023-01-01T00:00:00Z","prompt":"valid1"}
this is not json
{"timestamp":"2023-01-01T00:00:01Z","prompt":"valid2"}
`
	if err := os.WriteFile(tracker.filepath, []byte(malformedContent), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := tracker.LoadTopN(context.Background(), 10)
	if err != nil {
		t.Fatalf("LoadTopN failed: %v", err)
	}

	expected := []string{"valid2", "valid1"}
	assertPromptsMatch(t, got, expected)
}

func TestGlobalPromptTracker_Append_EmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	err := tracker.Append(context.Background(), "")
	if err != nil {
		t.Errorf("expected nil error for empty prompt, got %v", err)
	}

	// Verify file was not created or is empty
	if _, err := os.Stat(tracker.filepath); err == nil {
		content, _ := os.ReadFile(tracker.filepath)
		if len(content) > 0 {
			t.Errorf("expected empty file, got %d bytes", len(content))
		}
	}
}

func TestNoOpTracker_Close(t *testing.T) {
	tracker := NewNoOpTracker()
	if err := tracker.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalPromptTracker_LoadTopN_LimitZero(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()

	got, err := tracker.LoadTopN(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d prompts; want 0", len(got))
	}
}

func TestGlobalPromptTracker_LoadTopN_LimitNegative(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()

	got, err := tracker.LoadTopN(context.Background(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d prompts; want 0", len(got))
	}
}

func TestGlobalPromptTracker_Append_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tracker.Close() }()
	tr := tracker.(*globalPromptTracker)

	// Make the file read-only to cause write failure
	if err := os.WriteFile(tr.filepath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tr.filepath, 0444); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(tr.filepath, 0644) }()

	err := tracker.Append(context.Background(), "p1")
	if err == nil {
		t.Errorf("expected error on write to read-only file, got nil")
	}
}

func TestGlobalPromptTracker_ScanChunk_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	// File with no newlines
	_ = os.WriteFile(tracker.filepath, []byte(`{"prompt":"test"}`), 0644)
	got, _ := tracker.LoadTopN(context.Background(), 10)
	if len(got) != 1 || got[0] != "test" {
		t.Errorf("expected [test], got %v", got)
	}

	// File with many empty lines
	_ = os.WriteFile(tracker.filepath, []byte("\n\n\n\n"), 0644)
	got, _ = tracker.LoadTopN(context.Background(), 10)
	if len(got) != 0 {
		t.Errorf("expected empty results, got %v", got)
	}
}

func TestGlobalPromptTracker_CompactionFailToTrigger(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	// Force compacting to be true
	tracker.compacting.Store(true)
	// Add many entries to trigger compaction threshold
	largePrompt := string(bytes.Repeat([]byte("A"), 1000))
	for i := 0; i < 200; i++ {
		_ = tracker.Append(context.Background(), largePrompt)
	}
	// Should not trigger compaction because already compacting
}


func TestGlobalPromptTracker_ProcessReversedLines_EmptyPromptInJSON(t *testing.T) {
	tracker := &globalPromptTracker{}
	lines := [][]byte{
		[]byte(`{"prompt":""}`),
		[]byte(`{"prompt":"valid"}`),
	}
	seen := make(map[string]bool)
	result := tracker.processReversedLines(lines, seen, nil, 10)
	if len(result) != 1 || result[0].Prompt != "valid" {
		t.Errorf("expected 1 valid prompt, got %v", result)
	}
}

func TestGlobalPromptTracker_PerformCompactionPass_CreateTempFailure(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	defer func() { _ = tr.Close() }()
	tracker := tr.(*globalPromptTracker)

	_ = tracker.Append(context.Background(), "test")
	
	// Make output dir read-only to cause CreateTemp to fail
	outputDir := filepath.Join(tmpDir, "output")
	if err := os.Chmod(outputDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(outputDir, 0755) }()

	success := tracker.performCompactionPass(context.Background())
	if success {
		t.Error("expected performCompactionPass to fail when output dir is read-only")
	}
}
