// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

func TestScratchpadStore(t *testing.T) {
	ctx := context.Background()
	fs := storage.DefaultFileSystem

	t.Run("Write and Read", func(t *testing.T) {
		tempDir := t.TempDir()
		scratchFile := filepath.Join(tempDir, "scratchpad.md")
		store := NewScratchpadStore(fs, scratchFile)

		content := "Initial thoughts."
		_, err := store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": content,
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := store.ManageScratchpad(ctx, map[string]interface{}{
			"action": "read",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != content {
			t.Errorf("got %q, want %q", res.Text, content)
		}
	})

	t.Run("Append", func(t *testing.T) {
		tempDir := t.TempDir()
		scratchFile := filepath.Join(tempDir, "scratchpad.md")
		store := NewScratchpadStore(fs, scratchFile)

		if _, err := store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": "Line 1",
		}); err != nil {
			t.Fatal(err)
		}

		_, err := store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "append",
			"content": "Line 2",
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := store.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
		if err != nil {
			t.Fatal(err)
		}
		expected := "Line 1\nLine 2"
		if res.Text != expected {
			t.Errorf("got %q, want %q", res.Text, expected)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		tempDir := t.TempDir()
		scratchFile := filepath.Join(tempDir, "scratchpad.md")
		store := NewScratchpadStore(fs, scratchFile)

		if _, err := store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": "Something",
		}); err != nil {
			t.Fatal(err)
		}

		_, err := store.ManageScratchpad(ctx, map[string]interface{}{"action": "clear"})
		if err != nil {
			t.Fatal(err)
		}

		res, err := store.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "(Scratchpad is empty)" {
			t.Errorf("expected empty message, got %q", res.Text)
		}
	})

	t.Run("Persistence", func(t *testing.T) {
		tempDir := t.TempDir()
		scratchFile := filepath.Join(tempDir, "scratchpad.md")
		store1 := NewScratchpadStore(fs, scratchFile)

		if _, err := store1.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": "Persist me",
		}); err != nil {
			t.Fatal(err)
		}

		store2 := NewScratchpadStore(fs, scratchFile)
		err := store2.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}

		res, err := store2.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "Persist me" {
			t.Error("scratchpad was not persisted")
		}
	})
}

func TestScratchpadStore_Concurrency(t *testing.T) {
	tempDir := t.TempDir()
	scratchFile := filepath.Join(tempDir, "stress_scratch.md")
	store := NewScratchpadStore(storage.DefaultFileSystem, scratchFile)
	ctx := context.Background()

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(val int) {
			defer wg.Done()

			// 1. Write
			_, err := store.ManageScratchpad(ctx, map[string]interface{}{
				"action":  "write",
				"content": fmt.Sprintf("Writer %d", val),
			})
			if err != nil {
				t.Errorf("Write error (worker %d): %v", val, err)
			}

			// 2. Append
			_, err = store.ManageScratchpad(ctx, map[string]interface{}{
				"action":  "append",
				"content": fmt.Sprintf("Append %d", val),
			})
			if err != nil {
				t.Errorf("Append error (worker %d): %v", val, err)
			}

			// 3. Read
			_, err = store.ManageScratchpad(ctx, map[string]interface{}{
				"action": "read",
			})
			if err != nil {
				t.Errorf("Read error (worker %d): %v", val, err)
			}
		}(i)
	}
	wg.Wait()

	// Final verification - just ensure it doesn't crash and we can read it
	res, err := store.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
	if err != nil {
		t.Fatalf("Final read error: %v", err)
	}
	if res.Text == "" || res.Text == "(Scratchpad is empty)" {
		t.Error("Scratchpad should not be empty after concurrent writes/appends")
	}

	// Verify disk file
	data, err := storage.DefaultFileSystem.ReadFile(ctx, scratchFile)
	if err != nil {
		t.Fatalf("Failed to read scratchpad file: %v", err)
	}
	if string(data) != res.Text {
		t.Error("Memory and disk state mismatch")
	}
}
