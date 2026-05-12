// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// TestGlobalPromptTracker_MigrationReadFileError tests gap 1:
// copyFile failure during legacy migration when ReadFile returns an error.
func TestGlobalPromptTracker_MigrationReadFileError(t *testing.T) {
	tmpDir := t.TempDir()

	// Create legacy file on real filesystem
	legacyDir := filepath.Join(tmpDir, ".tellmego")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("failed to create legacy dir: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, "prompts.jsonl")
	if err := os.WriteFile(legacyPath, []byte(`{"timestamp":"t","prompt":"legacy"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to write legacy file: %v", err)
	}

	baseFS := persistence.NewOSFileSystem()
	mfs := &mockFS{FileSystem: baseFS, readErr: errors.New("injected read error")}

	tracker, err := NewGlobalPromptTracker(mfs, tmpDir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to migrate history file from") {
		t.Errorf("expected error containing %q, got %q", "failed to migrate history file from", err.Error())
	}
	if tracker == nil {
		t.Error("expected non-nil tracker (partial success return)")
	}
}

// TestGlobalPromptTracker_WriteCompactedDataFailure tests gap 2:
// json.Marshal failure in writeCompactedData (via errorWriter).
// json.Marshal on string-only promptEntry cannot fail naturally,
// so we test writeCompactedData directly with a failing writer.
func TestGlobalPromptTracker_WriteCompactedDataFailure(t *testing.T) {
	tracker := &globalPromptTracker{}
	entries := []promptEntry{
		{Timestamp: "2023-01-01T00:00:00Z", Prompt: "test1"},
		{Timestamp: "2023-01-01T00:00:01Z", Prompt: "test2"},
	}

	// errorWriter fails on every Write call
	success := tracker.writeCompactedData(&errorWriter{}, entries)
	if success {
		t.Error("expected writeCompactedData to return false with errorWriter")
	}
}

// TestGlobalPromptTracker_AppendWriteFailure tests gap 3:
// f.Write failure in Append (L141-143).
func TestGlobalPromptTracker_AppendWriteFailure(t *testing.T) {
	tmpDir := t.TempDir()

	baseFS := persistence.NewOSFileSystem()
	mfs := &mockFS{FileSystem: baseFS}

	// Create output directory on real filesystem (so OpenFile with O_CREATE can succeed)
	outputDir := filepath.Join(tmpDir, "output")
	if err := baseFS.MkdirAll(context.Background(), outputDir, 0755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: filepath.Join(outputDir, "global_prompts.jsonl"),
	}

	// Inject write error
	mfs.writeErr = errors.New("injected write error")

	err := tracker.Append(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to append prompt") {
		t.Errorf("expected error containing %q, got %q", "failed to append prompt", err.Error())
	}
}

// TestGlobalPromptTracker_StatErrorInLoadTopN tests gap 4:
// fs.Stat error in doLoadTopUniqueEntries (L206-208).
func TestGlobalPromptTracker_StatErrorInLoadTopN(t *testing.T) {
	tmpDir := t.TempDir()

	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := filepath.Join(outputDir, "global_prompts.jsonl")

	// Create output directory and seed the tracker file
	if err := baseFS.MkdirAll(context.Background(), outputDir, 0755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}
	if err := baseFS.WriteFile(context.Background(), trackerPath,
		[]byte(`{"timestamp":"t","prompt":"hello"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to seed tracker file: %v", err)
	}

	mfs := &mockFS{FileSystem: baseFS, statErr: errors.New("injected stat error")}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: trackerPath,
	}

	_, err := tracker.LoadTopN(context.Background(), 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to stat global prompts file") {
		t.Errorf("expected error containing %q, got %q", "failed to stat global prompts file", err.Error())
	}
}

// TestGlobalPromptTracker_ReadAtErrorInLoadTopN tests gaps 6+7:
// scanner.scanChunk() ReadAt error propagation (L227-229 and L253-255).
// Both gaps go through the same code path: scanChunk → ReadAt.
func TestGlobalPromptTracker_ReadAtErrorInLoadTopN(t *testing.T) {
	tmpDir := t.TempDir()

	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := filepath.Join(outputDir, "global_prompts.jsonl")

	// Create output directory and seed the tracker file
	if err := baseFS.MkdirAll(context.Background(), outputDir, 0755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}
	if err := baseFS.WriteFile(context.Background(), trackerPath,
		[]byte(`{"timestamp":"t","prompt":"hello"}`+"\n"), 0644); err != nil {
		t.Fatalf("failed to seed tracker file: %v", err)
	}

	mfs := &mockFS{FileSystem: baseFS, readAtErr: errors.New("injected readat error")}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: trackerPath,
	}

	_, err := tracker.LoadTopN(context.Background(), 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read global prompts at") {
		t.Errorf("expected error containing %q, got %q", "failed to read global prompts at", err.Error())
	}
}

// TestGlobalPromptTracker_ContextCancelledInLoadTopN tests gap 5:
// ctx.Done() cancellation in loadTopUniqueEntries loop (L221-222).
// Already covered by TestGlobalPromptTracker_LoadTopN_ContextCancelled
// in global_prompt_tracker_test.go (~line 260).
// This test exists as documentation and re-verification.
func TestGlobalPromptTracker_ContextCancelledInLoadTopN(t *testing.T) {
	tmpDir := t.TempDir()
	fs := persistence.NewOSFileSystem()
	tracker, err := NewGlobalPromptTracker(fs, tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPromptTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	_ = tracker.Append(context.Background(), "p1")
	_ = tracker.Append(context.Background(), "p2")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tracker.LoadTopN(ctx, 10)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
