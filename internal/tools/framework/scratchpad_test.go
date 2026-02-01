// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

func TestScratchpadStore(t *testing.T) {
	ctx := context.Background()
	fs := fsutil.DefaultFileSystem

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

		store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": "Line 1",
		})

		_, err := store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "append",
			"content": "Line 2",
		})
		if err != nil {
			t.Fatal(err)
		}

		res, _ := store.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
		expected := "Line 1\nLine 2"
		if res.Text != expected {
			t.Errorf("got %q, want %q", res.Text, expected)
		}
	})

	t.Run("Clear", func(t *testing.T) {
		tempDir := t.TempDir()
		scratchFile := filepath.Join(tempDir, "scratchpad.md")
		store := NewScratchpadStore(fs, scratchFile)

		store.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": "Something",
		})

		_, err := store.ManageScratchpad(ctx, map[string]interface{}{"action": "clear"})
		if err != nil {
			t.Fatal(err)
		}

		res, _ := store.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
		if res.Text != "(Scratchpad is empty)" {
			t.Errorf("expected empty message, got %q", res.Text)
		}
	})

	t.Run("Persistence", func(t *testing.T) {
		tempDir := t.TempDir()
		scratchFile := filepath.Join(tempDir, "scratchpad.md")
		store1 := NewScratchpadStore(fs, scratchFile)

		store1.ManageScratchpad(ctx, map[string]interface{}{
			"action":  "write",
			"content": "Persist me",
		})

		store2 := NewScratchpadStore(fs, scratchFile)
		err := store2.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}

		res, _ := store2.ManageScratchpad(ctx, map[string]interface{}{"action": "read"})
		if res.Text != "Persist me" {
			t.Error("scratchpad was not persisted")
		}
	})
}
