// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOutputFormattingFlags(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Setup Mock Server (shared — httptest.Server is goroutine-safe)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Use a unique string that includes markdown.
		resp := createTextResponse("google", "Text with **bold** and _italics_")
		_, _ = fmt.Fprint(w, resp)
	}))
	// Use t.Cleanup instead of defer so the server stays alive during parallel
	// subtests. (defer runs when the parent function returns, which happens
	// before t.Parallel subtests resume.)
	t.Cleanup(func() { server.Close() })

	// 2. Shared config (read-only, safe for parallel subtests)
	configPath := createTempConfig(t, "google", server.URL)
	mockURL := server.URL

	t.Run("RawOutput", func(t *testing.T) {
		t.Parallel()
		// Each subtest needs its own home dir to avoid SQLITE_BUSY when
		// two binaries initialize the persistence layer concurrently.
		homeDir := t.TempDir()
		env := []string{
			"TELL_ME_HOME=" + homeDir,
			"TELL_ME_MOCK_URL=" + mockURL,
		}
		// Test -r (Raw Output)
		stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "-r", "Format this")
		if err != nil {
			t.Fatalf("CLI -r failed: %v\nStderr: %s", err, stderr)
		}

		// Standard output should contain the raw markdown.
		if !strings.Contains(stdout, "**bold**") || !strings.Contains(stdout, "_italics_") {
			t.Errorf("Expected output to contain raw markdown, got: %q", stdout)
		}

		// Verify it does NOT contain ANSI escape codes.
		if strings.Contains(stdout, "\x1b[") {
			t.Errorf("Expected no ANSI escape codes in raw output, but found some: %q", stdout)
		}
	})

	t.Run("InteractiveFallback", func(t *testing.T) {
		t.Parallel()
		// Each subtest needs its own home dir to avoid SQLITE_BUSY when
		// two binaries initialize the persistence layer concurrently.
		homeDir := t.TempDir()
		env := []string{
			"TELL_ME_HOME=" + homeDir,
			"TELL_ME_MOCK_URL=" + mockURL,
		}
		// Test -i (Interactive/TUI Mode)
		// Fallback to standard input in a non-TTY environment.
		stdinContent := "Hello from piped stdin"
		stdout, stderr, err := runCommandWithEnv(env, stdinContent, "-c="+configPath, "-i")
		if err != nil {
			t.Fatalf("CLI -i failed: %v\nStderr: %s", err, stderr)
		}

		// Standard output should contain the model's response.
		// We use a loose match because markdown renderers might normalize formatting
		// (e.g., _ to *) or add spacing/padding.
		out := stripANSI(stdout)
		if !strings.Contains(out, "Text with") || !strings.Contains(out, "bold") {
			t.Errorf("Expected model response, got: %q", out)
		}

		// Check stderr for the fallback log.
		errOut := stripANSI(stderr)
		if !strings.Contains(errOut, "Input captured") {
			t.Errorf("Expected 'Input captured' in stderr, got: %q", errOut)
		}
	})
}
