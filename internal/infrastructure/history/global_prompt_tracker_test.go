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
	tracker := NewGlobalPromptTracker(tmpDir)

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
	tracker := NewGlobalPromptTracker(filepath.Join(tmpDir, "non-existent"))

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
	tracker := NewGlobalPromptTracker(tmpDir)

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
	tracker := NewGlobalPromptTracker(tmpDir)

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
	tracker = NewGlobalPromptTracker(t.TempDir()) // Reset
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
	tracker := NewGlobalPromptTracker(tmpDir).(*globalPromptTracker)

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
	tracker := NewGlobalPromptTracker(tmpDir).(*globalPromptTracker)

	// Append a lot of duplicates to exceed 150KB
	largePrompt := string(bytes.Repeat([]byte("A"), 1000))
	for i := 0; i < 200; i++ {
		_ = tracker.Append(largePrompt)
	}

	// Verify size crossed threshold (eventually, during the loop)
	// We don't check it here because compaction might have already started and shrunk the file.

	// Wait for async compaction to reduce the file to 1 unique line.
	// Since compaction is async and there's a race between Append and Rename,
	// we might end up with more than 1 line if an Append happened after the last Rename.
	// But it should definitely be much less than 200.
	var finalLines int
	for i := 0; i < 50; i++ {
		content, _ := os.ReadFile(tracker.filepath)
		lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
		finalLines = len(lines)
		if finalLines < 200 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if finalLines >= 200 {
		t.Errorf("file was not compacted: got %d lines; want < 200", finalLines)
	}
	if finalLines == 0 {
		t.Errorf("file should not be empty")
	}
}
