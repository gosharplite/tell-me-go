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
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

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

func TestHistoryNavigationFlags(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := createTextResponse("google", "Response to your prompt")
		_, _ = fmt.Fprint(w, resp)
	}))
	defer server.Close()

	// 2. Setup Environment
	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
		"TELL_ME_MOCK_ANSWER=y", // For --retry confirmation
	}

	histPath := filepath.Join(homeDir, "output", "assistant", "history.jsonl")

	// 3. Setup: Send 3 distinct prompts sequentially to populate session history.
	prompts := []string{"Message 1", "Message 2", "Message 3"}
	for _, p := range prompts {
		_, _, err := runCommandWithEnv(env, "", "-c="+configPath, p)
		if err != nil {
			t.Fatalf("Failed to send prompt %q: %v", p, err)
		}
		forceReconcileHistory(t, histPath)
		// Windows file lock mitigation
		time.Sleep(1000 * time.Millisecond)
	}

	t.Run("LastNMessages", func(t *testing.T) {
		forceReconcileHistory(t, histPath)
		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-l", "4")
		if err != nil {
			t.Fatalf("CLI -l 4 failed: %v\nStderr: %s", err, stderr)
		}

		out := stripANSI(stdout)
		if !strings.Contains(out, "Message 2") {
			t.Errorf("Expected output to contain 'Message 2', got: %q", out)
		}
		if !strings.Contains(out, "Message 3") {
			t.Errorf("Expected output to contain 'Message 3', got: %q", out)
		}
	})

	t.Run("GoBack", func(t *testing.T) {
		// Test -b (Go back / Undo)
		_, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-b", "1")
		if err != nil {
			t.Fatalf("CLI -b 1 failed: %v\nStderr: %s", err, stderr)
		}
		forceReconcileHistory(t, histPath)
		time.Sleep(1000 * time.Millisecond)

		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-l", "4")
		if err != nil {
			t.Fatalf("CLI -l 4 after -b 1 failed: %v\nStderr: %s", err, stderr)
		}

		out := stripANSI(stdout)
		if !strings.Contains(out, "Message 1") {
			t.Errorf("Expected output to contain 'Message 1', got: %q", out)
		}
		if !strings.Contains(out, "Message 2") {
			t.Errorf("Expected output to contain 'Message 2', got: %q", out)
		}
		if strings.Contains(out, "Message 3") {
			t.Errorf("Expected output NOT to contain 'Message 3' after -b 1, got: %q", out)
		}
	})

	t.Run("Retry", func(t *testing.T) {
		// Test --retry
		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "--retry")
		if err != nil {
			t.Fatalf("CLI --retry failed: %v\nStderr: %s", err, stderr)
		}
		forceReconcileHistory(t, histPath)
		time.Sleep(1000 * time.Millisecond)

		out := stripANSI(stdout)
		if !strings.Contains(out, "Response to your prompt") {
			t.Errorf("Expected model response, got: %q", out)
		}

		content, err := os.ReadFile(histPath)
		if err != nil {
			t.Fatalf("Failed to read history file: %v", err)
		}

		histStr := string(content)
		if !strings.Contains(histStr, "Message 2") {
			t.Errorf("Expected history to contain 'Message 2', got: %q", histStr)
		}
		if strings.Contains(histStr, "Message 3") {
			t.Errorf("Expected history NOT to contain 'Message 3', got: %q", histStr)
		}
	})
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
	time.Sleep(500 * time.Millisecond)

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
		time.Sleep(500 * time.Millisecond)

		if !strings.Contains(stdout, "Rolled back 1 turns") {
			t.Error("Expected stdout to contain rollback confirmation")
		}
	})
}
