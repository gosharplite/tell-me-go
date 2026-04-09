// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	// 3. Setup: Send 3 distinct prompts sequentially to populate session history.
	// Each turn adds 2 messages to the history: 1 User prompt and 1 Model response.
	prompts := []string{"Message 1", "Message 2", "Message 3"}
	for _, p := range prompts {
		_, _, err := runCommandWithEnv(env, "", "-c="+configPath, p)
		if err != nil {
			t.Fatalf("Failed to send prompt %q: %v", p, err)
		}
	}

	t.Run("LastNMessages", func(t *testing.T) {
		// Test -l (Last N messages)
		// NOTE: In this implementation, each turn consists of 2 messages (User + Model).
		// To see both 'Message 2' and 'Message 3' user prompts, we need to request the last 4 messages.
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
		if strings.Contains(out, "Message 1") {
			t.Errorf("Expected output NOT to contain 'Message 1', got: %q", out)
		}
	})

	t.Run("GoBack", func(t *testing.T) {
		// Test -b (Go back / Undo)
		// Execute with -b 1 to delete the last turn (Turn 3 / Message 3).
		_, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-b", "1")
		if err != nil {
			t.Fatalf("CLI -b 1 failed: %v\nStderr: %s", err, stderr)
		}

		// Verify that Message 3 was successfully deleted and we now see Message 1 and 2.
		// We use -l 4 to see both remaining turns (Message 1 and Message 2).
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
		// Execute with --retry. Since we went back 1, the last message is "Message 2".
		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "--retry")
		if err != nil {
			t.Fatalf("CLI --retry failed: %v\nStderr: %s", err, stderr)
		}

		out := stripANSI(stdout)
		if !strings.Contains(out, "Response to your prompt") {
			t.Errorf("Expected model response, got: %q", out)
		}

		// Verify history now contains Message 2 as the last user message.
		histPath := filepath.Join(homeDir, "output", "assistant", "history.jsonl")
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
