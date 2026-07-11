// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// historyNavEnv holds the shared test environment for history navigation E2E tests.
type historyNavEnv struct {
	env        []string
	configPath string
	histPath   string
}

// newHistoryNavEnv creates a mock server, temp config, env vars, and populates
// the session history with 3 sequential prompts ("Message 1", "Message 2", "Message 3").
func newHistoryNavEnv(t *testing.T) *historyNavEnv {
	t.Helper()

	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := createTextResponse("google", "Response to your prompt")
		_, _ = fmt.Fprint(w, resp)
	}))
	t.Cleanup(server.Close)

	// 2. Setup Environment
	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
		"TELL_ME_MOCK_ANSWER=y",
	}

	histPath := filepath.Join(homeDir, "output", "assistant", "history.jsonl")

	// 3. Populate session history with 3 distinct prompts directly via
	// history.Manager (avoids 3 subprocess invocations at ~200ms each).
	fs := persistence.NewOSFileSystem()
	mgr := history.NewManager(fs, histPath, histPath+".archive")
	if err := mgr.Load(context.Background()); err != nil {
		t.Fatalf("Failed to load history: %v", err)
	}

	prompts := []string{"Message 1", "Message 2", "Message 3"}
	for _, p := range prompts {
		if err := mgr.AddContent(context.Background(), &llm.Content{
			Role:  "user",
			ID:    llm.NewID(),
			Parts: []*llm.Part{{Text: p}},
		}); err != nil {
			t.Fatalf("Failed to add user content for %q: %v", p, err)
		}
		if err := mgr.AddContent(context.Background(), &llm.Content{
			Role:  "model",
			ID:    llm.NewID(),
			Parts: []*llm.Part{{Text: "Response to your prompt"}},
		}); err != nil {
			t.Fatalf("Failed to add model content for %q: %v", p, err)
		}
	}
	if err := mgr.Sync(context.Background()); err != nil {
		t.Fatalf("Failed to sync history: %v", err)
	}

	return &historyNavEnv{
		env:        env,
		configPath: configPath,
		histPath:   histPath,
	}
}

// waitForHistoryStable blocks until the history file is readable and
// its size has stopped changing (two consecutive polls match).
//
// Platform behavior:
//   - POSIX (Linux/macOS): cmd.Wait() guarantees child file handles are
//     closed; a single os.Stat existence check is sufficient.
//   - Windows: AV and filter drivers may hold file handles for 50-500ms
//     after process exit. A short polling loop (up to 500ms) handles this
//     without adding latency to POSIX CI runs.
func waitForHistoryStable(t *testing.T, path string) {
	t.Helper()

	if runtime.GOOS != "windows" {
		// POSIX: cmd.Wait() guarantees file availability.
		if _, err := os.Stat(path); err != nil {
			t.Logf("history file %s not found after process exit: %v", path, err)
		}
		return
	}

	// Windows: poll until file size stabilizes (AV/filter drivers may
	// delay handle release after process exit).
	const pollInterval = 10 * time.Millisecond
	const pollTimeout = 500 * time.Millisecond

	deadline := time.Now().Add(pollTimeout)
	var lastSize int64 = -1

	for time.Now().Before(deadline) {
		fi, err := os.Stat(path)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if fi.Size() == lastSize {
			return // stable
		}
		lastSize = fi.Size()
		time.Sleep(pollInterval)
	}

	// Timeout is non-fatal: log a warning but don't fail.
	t.Logf("history file %s did not stabilize within %v (final size: %d)",
		path, pollTimeout, lastSize)
}

// forceReconcileHistory ensures the history file is fully flushed and visible on Windows.
func forceReconcileHistory(t *testing.T, path string) {
	t.Helper()
	// Only needed on Windows to handle potential lazy writes or cache delays in E2E tests
	fs := persistence.NewOSFileSystem()
	mgr := history.NewManager(fs, path, path+".archive")
	// Load and Sync will force the OS to reconcile the file state
	if err := mgr.Load(context.Background()); err != nil {
		t.Logf("Debug: load during reconciliation: %v", err)
	}
	if err := mgr.Sync(context.Background()); err != nil {
		t.Logf("Debug: sync during reconciliation: %v", err)
	}
}

// TestHistoryNavigation_CompleteWorkflow exercises the full history
// navigation lifecycle in a single sequential flow:
//  1. List last N messages (read-only verification of populated history)
//  2. Roll back (-b) and verify truncated history
//  3. Retry (--retry) from rolled-back state and verify model response
//
// These are not independent subtests because steps 2 and 3 mutate
// shared state and step 3 depends on step 2's rollback. Running them
// as a single procedural test avoids subtest-isolation violations
// while preserving the speed win of a single newHistoryNavEnv call.
func TestHistoryNavigation_CompleteWorkflow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	env := newHistoryNavEnv(t)

	// ── Step 1: List last N messages ──
	forceReconcileHistory(t, env.histPath)
	stdout, stderr, err := runCommandWithEnv(env.env, "", "-c="+env.configPath, "-l", "4")
	if err != nil {
		t.Fatalf("CLI -l 4 failed: %v\nStderr: %s", err, stderr)
	}
	out := stripANSI(stdout)
	if !strings.Contains(out, "Message 2") {
		t.Errorf("Step 1 -l: expected output to contain 'Message 2', got: %q", out)
	}
	if !strings.Contains(out, "Message 3") {
		t.Errorf("Step 1 -l: expected output to contain 'Message 3', got: %q", out)
	}

	// ── Step 2: Go back (-b 1) ──
	_, stderr, err = runCommandWithEnv(env.env, "", "-c="+env.configPath, "-b", "1")
	if err != nil {
		t.Fatalf("CLI -b 1 failed: %v\nStderr: %s", err, stderr)
	}
	forceReconcileHistory(t, env.histPath)
	waitForHistoryStable(t, env.histPath)

	stdout, stderr, err = runCommandWithEnv(env.env, "", "-c="+env.configPath, "-l", "4")
	if err != nil {
		t.Fatalf("CLI -l 4 after -b 1 failed: %v\nStderr: %s", err, stderr)
	}
	out = stripANSI(stdout)
	if !strings.Contains(out, "Message 1") {
		t.Errorf("Step 2 -b: expected output to contain 'Message 1', got: %q", out)
	}
	if !strings.Contains(out, "Message 2") {
		t.Errorf("Step 2 -b: expected output to contain 'Message 2', got: %q", out)
	}
	if strings.Contains(out, "Message 3") {
		t.Errorf("Step 2 -b: expected output NOT to contain 'Message 3' after -b 1, got: %q", out)
	}

	// ── Step 3: Retry (--retry) from rolled-back state ──
	// The rolled-back state from Step 2 is the prerequisite.
	stdout, stderr, err = runCommandWithEnv(env.env, "", "-c="+env.configPath, "--retry")
	if err != nil {
		t.Fatalf("CLI --retry failed: %v\nStderr: %s", err, stderr)
	}
	forceReconcileHistory(t, env.histPath)
	waitForHistoryStable(t, env.histPath)

	out = stripANSI(stdout)
	if !strings.Contains(out, "Response to your prompt") {
		t.Errorf("Step 3 --retry: expected model response, got: %q", out)
	}

	content, err := os.ReadFile(env.histPath)
	if err != nil {
		t.Fatalf("Step 3: failed to read history file: %v", err)
	}
	histStr := string(content)
	if !strings.Contains(histStr, "Message 2") {
		t.Errorf("Step 3: expected history to contain 'Message 2', got: %q", histStr)
	}
	if strings.Contains(histStr, "Message 3") {
		t.Errorf("Step 3: expected history NOT to contain 'Message 3', got: %q", histStr)
	}
}

func TestHistoryOnlyExit(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	histPath := filepath.Join(homeDir, "output", "assistant", "history.jsonl")

	_, _, _ = runCommandWithEnv(env, "", "-c="+configPath, "initial message")
	forceReconcileHistory(t, histPath)
	waitForHistoryStable(t, histPath)

	t.Run("ShowHistoryAndExit", func(t *testing.T) {
		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-l")
		if err != nil {
			t.Fatalf("CLI -l failed: %v\nStderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "initial message") {
			t.Error("Expected stdout to contain history")
		}
	})

	t.Run("RollbackAndExit", func(t *testing.T) {
		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-b", "1")
		if err != nil {
			t.Fatalf("CLI -b 1 failed: %v\nStderr: %s", err, stderr)
		}
		forceReconcileHistory(t, histPath)
		waitForHistoryStable(t, histPath)

		if !strings.Contains(stdout, "Rolled back 1 turns") {
			t.Error("Expected stdout to contain rollback confirmation")
		}
	})
}
