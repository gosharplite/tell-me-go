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

func TestTurnHeaderFormat(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// TODO: Choose a provider (e.g., "google")
	provider := "google"

	// TODO: Setup mock LLM server that returns a simple text response
	// (no tool calls) to trigger a single turn.
	server := setupSimpleTextMockServer(t, provider, "Hello, this is a simple text response.")
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Run the CLI with a prompt.
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "Say hello")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// Capture stderr and strip ANSI codes.
	errOut := stripANSI(stderr)

	// TODO: Assert that stderr contains, in order:
	// - An empty line (or newline before the horizontal rule)
	// - The horizontal rule (────────────────────────────────────────────────────────────────────────────────)
	// - The turn line (╭─⠿ Turn X - mode)
	// - The payload line ([timestamp] Payload: ~NNN/MMM tokens - mode)
	t.Logf("Stderr output (stripped):\n%s", errOut)
}

// TODO: Implement setupSimpleTextMockServer that returns a server that responds with a simple text message.
func setupSimpleTextMockServer(t *testing.T, provider, text string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate endpoint mapping based on provider (same as existing)
		if provider == "google" && !strings.Contains(r.URL.Path, "generateContent") {
			t.Errorf("Google provider should use generateContent endpoint, got: %s", r.URL.Path)
			return
		}
		if provider == "openai" && !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("OpenAI provider should use /chat/completions endpoint, got: %s", r.URL.Path)
			return
		}
		if provider == "anthropic" && !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("Anthropic provider should use /messages endpoint, got: %s", r.URL.Path)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// Always return a simple text response (no tool calls)
		response := createTextResponse(provider, text)
		t.Logf("MOCK_SERVER_RESPONSE [%s]: %s\n", provider, response)
		_, _ = fmt.Fprint(w, response)
	}))
	return server
}
