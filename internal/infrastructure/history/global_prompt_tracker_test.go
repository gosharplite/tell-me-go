// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"path/filepath"
	"testing"
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
	got, err := tracker.LoadTopN(5)
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
	got2, err := tracker.LoadTopN(2)
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

	got, err := tracker.LoadTopN(10)
	if err != nil {
		t.Fatalf("LoadTopN from non-existent file failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d prompts; want 0", len(got))
	}
}
