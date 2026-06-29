// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResilience_504Retry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			w.Header().Set("Content-Type", "application/json")
			// Return a message that specifically avoids current regex matches but represents a 504
			if _, err := fmt.Fprint(w, `{"error": {"code": 504, "message": "The server encountered a temporary error and could not complete your request."}}`); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// Use Google provider format for the successful response
		resp := createTextResponse("google", "The operation succeeded after retry.")
		if _, err := fmt.Fprint(w, resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Run the command
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "retry test")

	// Assertions
	errOut := stripANSI(stderr)
	out := stripANSI(stdout)

	// EXPECTED TO FAIL IF REGRESSION IS PRESENT:
	// The command must not exit with a non-zero code on the first 504.
	if err != nil {
		t.Fatalf("CLI failed prematurely (Regression Reproduced): %v\nStderr: %s", err, errOut)
	}

	// The stderr must contain a warning message matching the system's retry notification:
	expectedWarning := "Transient error"
	if !strings.Contains(errOut, expectedWarning) {
		t.Errorf("Expected stderr to contain %q, got: %q", expectedWarning, errOut)
	}

	if !strings.Contains(errOut, "(Attempt 1)") {
		t.Errorf("Expected stderr to contain \"(Attempt 1)\", got: %q", errOut)
	}

	// The stdout must eventually contain the success message from the second attempt.
	expectedSuccess := "The operation succeeded after retry."
	if !strings.Contains(out, expectedSuccess) {
		t.Errorf("Expected stdout to contain %q, got: %q", expectedSuccess, out)
	}

	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount < 2 {
		t.Errorf("Expected at least 2 requests (system did not retry), got %d", finalCount)
	}
}

func TestResilience_429Retry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Content-Type", "application/json")
			// Return a message that specifically avoids legacy regex matches but represents a 429
			if _, err := fmt.Fprint(w, `{"error": {"code": 429, "message": "Error 429: Rate limit reached"}}`); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := createTextResponse("google", "The operation succeeded after rate limit retry.")
		if _, err := fmt.Fprint(w, resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Run the command
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "rate limit test")

	// Assertions
	errOut := stripANSI(stderr)
	out := stripANSI(stdout)

	if err != nil {
		t.Fatalf("CLI failed prematurely on rate limit: %v\nStderr: %s", err, errOut)
	}

	// The stderr must contain a warning message matching the system's retry notification
	if !strings.Contains(errOut, "Transient error") && !strings.Contains(errOut, "rate limit exceeded") {
		t.Errorf("Expected retry warning in stderr, but got: %q", errOut)
	}

	if !strings.Contains(out, "The operation succeeded after rate limit retry.") {
		t.Errorf("Expected success message in stdout, but got: %q", out)
	}

	finalCount := atomic.LoadInt32(&requestCount)
	if finalCount < 2 {
		t.Errorf("Expected at least 2 requests, got %d", finalCount)
	}
}
