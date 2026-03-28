// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGlobalPromptTracker(t *testing.T) {
	tmpDir := t.TempDir()
	tracker, _ := NewGlobalPromptTracker(tmpDir)

	prompts := []string{"hello", "world", "hello", "foo", "bar", "hello"}
	for _, p := range prompts {
		if err := tracker.Append(p); err != nil {
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
	if len(got) != len(expected) {
		t.Errorf("got %d prompts; want %d", len(got), len(expected))
	}
	for i, v := range got {
		if v != expected[i] {
			t.Errorf("at index %d: got %q; want %q", i, v, expected[i])
		}
	}

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

	// Create a payload larger than 64KB (e.g., 70,000 chars)
	// We'll use a string that's clearly larger than bufio.MaxScanTokenSize (64*1024)
	largePrompt := "START_" + string(make([]byte, 70000)) + "_END"

	err := tracker.Append(largePrompt)
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

	// Add 10 "duplicate" prompts
	for i := 0; i < 10; i++ {
		if err := tracker.Append("duplicate"); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Add 5 unique prompts BEFORE the duplicates
	uniquePrompts := []string{"p1", "p2", "p3", "p4", "p5"}
	// We append them first, so they are at the beginning of the file.
	// But to test that it returns them even if they are far back:
	tracker, _ = NewGlobalPromptTracker(t.TempDir()) // Reset
	for _, p := range uniquePrompts {
		if err := tracker.Append(p); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := tracker.Append("duplicate"); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Should return ["duplicate", "p5", "p4", "p3", "p2"]
	limit := 5
	got, err := tracker.LoadTopN(context.Background(), limit)
	if err != nil {
		t.Fatalf("LoadTopN failed: %v", err)
	}

	expected := []string{"duplicate", "p5", "p4", "p3", "p2"}
	if len(got) != len(expected) {
		t.Errorf("got %d prompts; want %d", len(got), len(expected))
	}
	for i, v := range got {
		if i < len(expected) && v != expected[i] {
			t.Errorf("at index %d: got %q; want %q", i, v, expected[i])
		}
	}
}

func TestGlobalPromptTracker_Compaction(t *testing.T) {
	tmpDir := t.TempDir()
	tr, _ := NewGlobalPromptTracker(tmpDir)
	tracker := tr.(*globalPromptTracker)

	// Add duplicates and many entries
	for i := 0; i < 10; i++ {
		_ = tracker.Append("duplicate")
	}
	_ = tracker.Append("unique1")
	_ = tracker.Append("unique2")
	_ = tracker.Append("duplicate") // latest one

	// Manually trigger compaction
	tracker.compactLog()

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
	tracker := tr.(*globalPromptTracker)

	// Append a lot of duplicates to exceed 150KB
	largePrompt := string(bytes.Repeat([]byte("A"), 1000))
	for i := 0; i < 200; i++ {
		_ = tracker.Append(largePrompt)
	}

	// Verify size crossed threshold (eventually, during the loop)
	// We don't check it here because compaction might have already started and shrunk the file.

	// Wait up to 3 seconds for async compaction
	timeout := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var finalLines int
loop:
	for {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for file to compact: got %d lines", finalLines)
		case <-ticker.C:
			content, err := os.ReadFile(tracker.filepath)
			if err != nil {
				continue // File might be locked/in-transition briefly
			}
			lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
			finalLines = len(lines)
			if finalLines < 200 {
				break loop
			}
		}
	}

	if finalLines == 0 {
		t.Errorf("file should not be empty")
	}
}

func TestGlobalPromptTracker_Migration(t *testing.T) {
	t.Run("successful migration", func(t *testing.T) {
		tmpDir := t.TempDir()
		legacyFile := filepath.Join(tmpDir, "global_prompts.jsonl")
		newFile := filepath.Join(tmpDir, ".tellmego", "prompts.jsonl")

		// 1. Setup legacy data
		legacyContent := []byte(`{"timestamp":"2023-01-01T00:00:00Z","prompt":"legacy test"}` + "\n")
		if err := os.WriteFile(legacyFile, legacyContent, 0644); err != nil {
			t.Fatalf("failed to write legacy file: %v", err)
		}

		// 2. Trigger migration
		_, _ = NewGlobalPromptTracker(tmpDir)

		// 3. Verify legacy file is gone
		if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
			t.Errorf("expected legacy file to be removed, got err: %v", err)
		}

		// 4. Verify new file exists and content matches
		if _, err := os.Stat(newFile); os.IsNotExist(err) {
			t.Fatalf("expected new file to exist at %s", newFile)
		}

		migratedContent, err := os.ReadFile(newFile)
		if err != nil {
			t.Fatalf("failed to read migrated file: %v", err)
		}

		if !bytes.Equal(legacyContent, migratedContent) {
			t.Errorf("content mismatch.\nwant: %s\ngot:  %s", legacyContent, migratedContent)
		}
	})

	t.Run("no migration if new file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		legacyFile := filepath.Join(tmpDir, "global_prompts.jsonl")
		newDir := filepath.Join(tmpDir, ".tellmego")
		newFile := filepath.Join(newDir, "prompts.jsonl")

		// 1. Setup legacy and existing data
		legacyContent := []byte(`{"timestamp":"2023-01-01T00:00:00Z","prompt":"legacy test"}` + "\n")
		if err := os.WriteFile(legacyFile, legacyContent, 0644); err != nil {
			t.Fatalf("failed to write legacy file: %v", err)
		}

		_ = os.MkdirAll(newDir, 0755)
		newContent := []byte(`{"timestamp":"2024-01-01T00:00:00Z","prompt":"new test"}` + "\n")
		if err := os.WriteFile(newFile, newContent, 0644); err != nil {
			t.Fatalf("failed to write new file: %v", err)
		}

		// 2. Trigger migration attempt
		_, _ = NewGlobalPromptTracker(tmpDir)

		// 3. Verify legacy file is STILL THERE (no migration)
		if _, err := os.Stat(legacyFile); os.IsNotExist(err) {
			t.Errorf("expected legacy file to still exist")
		}

		// 4. Verify new file content is NOT overwritten
		migratedContent, err := os.ReadFile(newFile)
		if err != nil {
			t.Fatalf("failed to read new file: %v", err)
		}

		if !bytes.Equal(newContent, migratedContent) {
			t.Errorf("content mismatch.\nwant: %s\ngot:  %s", newContent, migratedContent)
		}
	})
}
