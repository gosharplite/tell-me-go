// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package history

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainpersistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
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

// ---------------------------------------------------------------------------
// Compaction Error Paths (Gaps 14–17)
// ---------------------------------------------------------------------------

// seedLargeTrackerFile creates a tracker file >150KB on the real filesystem
// and returns its path. Used by compaction error path tests that need the
// retry loop to see the file as still-over-threshold.
func seedLargeTrackerFile(t *testing.T, baseFS domainpersistence.FileSystem, outputDir string) string {
	t.Helper()
	trackerPath := filepath.Join(outputDir, "global_prompts.jsonl")
	if err := baseFS.MkdirAll(context.Background(), outputDir, 0755); err != nil {
		t.Fatalf("failed to create output dir: %v", err)
	}

	largePrompt := strings.Repeat("A", 1000)
	var buf bytes.Buffer
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&buf, `{"timestamp":"t","prompt":"%s%d"}`, largePrompt, i)
		buf.WriteByte('\n')
	}
	if err := baseFS.WriteFile(context.Background(), trackerPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("failed to seed large tracker file: %v", err)
	}
	return trackerPath
}

// TestGlobalPromptTracker_CompactLog_StatErrorInRetry tests gap 14:
// compactLog retry loop — fs.Stat error (L310-312).
// When performCompactionPass returns false AND the retry Stat also fails,
// compactLog releases the compacting flag and returns without panic.
func TestGlobalPromptTracker_CompactLog_StatErrorInRetry(t *testing.T) {
	tmpDir := t.TempDir()
	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := seedLargeTrackerFile(t, baseFS, outputDir)

	// statErr causes both performCompactionPass.Stat and the retry-loop Stat to fail
	mfs := &mockFS{FileSystem: baseFS, statErr: errors.New("injected stat error")}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: trackerPath,
	}

	// compactLog manages its own compacting flag
	tracker.compacting.Store(true)
	tracker.compactLog(context.Background())

	if tracker.compacting.Load() {
		t.Error("expected compacting to be released after Stat error aborts retry")
	}

	// Verify original file is untouched (no data loss)
	content, err := baseFS.ReadFile(context.Background(), trackerPath)
	if err != nil {
		t.Fatalf("failed to read file after aborted compaction: %v", err)
	}
	if len(content) == 0 {
		t.Error("file should not be empty after aborted compaction")
	}
}

// TestGlobalPromptTracker_CompactLog_ContextCancelledInRetry tests gap 15:
// compactLog retry loop — ctx.Done() cancellation during backoff (L316-317).
// When performCompactionPass returns false and the context is cancelled,
// the select in the retry loop picks ctx.Done() and returns immediately.
func TestGlobalPromptTracker_CompactLog_ContextCancelledInRetry(t *testing.T) {
	tmpDir := t.TempDir()
	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := seedLargeTrackerFile(t, baseFS, outputDir)

	// Use a clean mockFS (no errors on Stat/Open) — the pre-cancelled context
	// causes doLoadTopUniqueEntries to fail with context.Canceled, which makes
	// performCompactionPass return false. The retry Stat succeeds, then
	// ctx.Done() fires in the select.
	mfs := &mockFS{FileSystem: baseFS}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: trackerPath,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so both doLoadTopUniqueEntries and retry select see it

	tracker.compacting.Store(true)

	// Use a channel to verify the function returns (doesn't hang on time.After)
	done := make(chan struct{})
	go func() {
		tracker.compactLog(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Success — returned promptly
	case <-time.After(2 * time.Second):
		t.Fatal("compactLog blocked despite cancelled context")
	}

	if tracker.compacting.Load() {
		t.Error("expected compacting to be released after context cancellation")
	}
}

// TestGlobalPromptTracker_PerformCompactionPass_StatError tests gap 16:
// performCompactionPass — initial fs.Stat error (L329-331).
// When the initial Stat call fails, performCompactionPass returns false
// immediately without modifying any state.
func TestGlobalPromptTracker_PerformCompactionPass_StatError(t *testing.T) {
	tmpDir := t.TempDir()
	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := seedLargeTrackerFile(t, baseFS, outputDir)

	// Read original content for post-condition check
	originalContent, err := baseFS.ReadFile(context.Background(), trackerPath)
	if err != nil {
		t.Fatalf("failed to read original file: %v", err)
	}

	mfs := &mockFS{FileSystem: baseFS, statErr: errors.New("injected stat error")}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: trackerPath,
	}

	success := tracker.performCompactionPass(context.Background())
	if success {
		t.Error("expected performCompactionPass to return false on Stat error")
	}

	// Verify file is untouched
	content, err := baseFS.ReadFile(context.Background(), trackerPath)
	if err != nil {
		t.Fatalf("failed to read file after aborted pass: %v", err)
	}
	if !bytes.Equal(content, originalContent) {
		t.Error("file content should be unchanged after Stat error aborts pass")
	}
}

// TestGlobalPromptTracker_PerformCompactionPass_LoadError tests gap 17:
// performCompactionPass — doLoadTopUniqueEntries failure (L336-338).
// When reading entries fails (e.g., ReadAt error in scanChunk),
// performCompactionPass returns false before acquiring the lock.
func TestGlobalPromptTracker_PerformCompactionPass_LoadError(t *testing.T) {
	tmpDir := t.TempDir()
	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := seedLargeTrackerFile(t, baseFS, outputDir)

	// readAtErr causes doLoadTopUniqueEntries → scanChunk → ReadAt to fail
	mfs := &mockFS{FileSystem: baseFS, readAtErr: errors.New("injected readat error")}

	tracker := &globalPromptTracker{
		fs:       mfs,
		filepath: trackerPath,
	}

	success := tracker.performCompactionPass(context.Background())
	if success {
		t.Error("expected performCompactionPass to return false on doLoadTopUniqueEntries error")
	}

	// Verify no data loss
	content, err := baseFS.ReadFile(context.Background(), trackerPath)
	if err != nil {
		t.Fatalf("failed to read file after aborted pass: %v", err)
	}
	if len(content) == 0 {
		t.Error("file should not be empty after aborted compaction pass")
	}
}

// TestGlobalPromptTracker_WriteCompactedDataFailsInCompactionPass verifies
// that writeCompactedData returns false when the underlying io.Writer fails.
// The performCompactionPass L353-355 return-false branch is unreachable with
// bytes.Buffer (which cannot fail), but this test validates the contract that
// callers must handle writeCompactedData returning false.
func TestGlobalPromptTracker_WriteCompactedDataFailsInCompactionPass(t *testing.T) {
	// Verify writeCompactedData contract with a failing writer
	tracker := &globalPromptTracker{}
	entries := []promptEntry{
		{Timestamp: "2026-01-01T00:00:00Z", Prompt: "test"},
	}

	// errorWriter is already defined in global_prompt_tracker_test.go
	if tracker.writeCompactedData(&errorWriter{}, entries) {
		t.Error("expected writeCompactedData to return false with errorWriter")
	}

	// Verify happy path: performCompactionPass with real data
	tmpDir := t.TempDir()
	baseFS := persistence.NewOSFileSystem()
	outputDir := filepath.Join(tmpDir, "output")
	trackerPath := seedLargeTrackerFile(t, baseFS, outputDir)

	realTracker := &globalPromptTracker{
		fs:       baseFS,
		filepath: trackerPath,
	}

	success := realTracker.performCompactionPass(context.Background())
	if !success {
		t.Error("expected performCompactionPass to succeed with valid data")
	}
}
