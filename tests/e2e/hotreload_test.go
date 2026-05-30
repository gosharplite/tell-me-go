// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// setupHotReloadSingleProcessMock returns an httptest.Server implementing a
// multi-turn Google-format mock and an atomic call counter. The server:
//
//   - Turn 0 (first LLM call)  → list_files tool call
//   - Turn 1 (second LLM call) → rewrites configPath to MAX_TURNS: 3,
//     then returns read_files tool call
//   - Turn 2+                    → text response "Done."
//
// The config path must be known before creating the mock so the handler
// can mutate it mid-session. The mock URL is obtained from server.URL at
// handler invocation time.
//
// The turn index is derived from the Google API contents array length:
//
//	idx := (len(contents) - 1) / 2
func setupHotReloadSingleProcessMock(t *testing.T, configPath string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var callCount atomic.Int32
	var configRewritten atomic.Bool
	var serverPtr atomic.Pointer[httptest.Server]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("HOTRELOAD_MOCK: failed to read body: %v", err)
			return
		}

		var body map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			t.Logf("HOTRELOAD_MOCK: failed to parse JSON: %v", err)
			return
		}

		contents, ok := body["contents"].([]interface{})
		if !ok {
			contents = nil
		}

		idx := (len(contents) - 1) / 2
		if idx < 0 {
			idx = 0
		}

		// On the second LLM call (turn 1), rewrite config to MAX_TURNS: 3
		// before returning the read_files tool call. The agent will detect
		// the ModTime change via ConfigWatcher.Refresh() before executing
		// the tool, allowing the read_files call to succeed.
		if count == 2 && !configRewritten.Swap(true) {
			srv := serverPtr.Load()
			if srv != nil {
				writeConfig(t, configPath, srv.URL, 3)
			}
		}

		var response string
		switch idx {
		case 0:
			response = createToolCallResponse("google", "list_files", map[string]interface{}{
				"path":   ".",
				"reason": "hot-reload turn 1",
			})
		case 1:
			response = createToolCallResponse("google", "read_files", map[string]interface{}{
				"filepaths": []interface{}{"test.txt"},
				"reason":    "hot-reload turn 2",
			})
		default:
			response = createTextResponse("google", "Done.")
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, response); err != nil {
			t.Logf("HOTRELOAD_MOCK: failed to write response: %v", err)
		}
	}))

	// Store the server pointer for the handler closure to use.
	serverPtr.Store(server)

	return server, &callCount
}

// writeConfig writes a YAML configuration file at path with the given
// mock server URL and MAX_TURNS value.
func writeConfig(t *testing.T, path, mockURL string, maxTurns int) {
	t.Helper()
	content := fmt.Sprintf(`
MODE: "assistant"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "google"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gemini-3-flash-preview"
MAX_TURNS: %d
MAX_HISTORY_TOKENS: 1000000
`, mockURL, maxTurns)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
}

// TestConfigHotReload_MaxTurns verifies that MAX_TURNS is dynamically
// reloaded from the config file mid-session within a single CLI invocation.
// The mock HTTP server rewrites the config file when it receives the second
// LLM request, increasing MAX_TURNS from 1 to 3. The agent detects the
// ModTime change via ConfigWatcher.Refresh() between the inference and
// execution phases, allowing the third tool call to proceed.
//
// Sequence:
//   - Turn 0: LLM returns list_files → executor runs it (turn 0 < MAX_TURNS 1)
//   - Between turns: Mock rewrites config to MAX_TURNS: 3 on second LLM call
//   - Turn 1: Refresh() detects ModTime change → maxToolTurns becomes 3 →
//     read_files is allowed (turn 1 < 3)
//   - Turn 2: LLM returns "Done." → agent exits cleanly
func TestConfigHotReload_MaxTurns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()

	// Step 1: Setup config path (must be known before mock creation)
	configPath := filepath.Join(homeDir, "config.yaml")

	// Step 2: Create mock server that rewrites config on second LLM call
	server, callCount := setupHotReloadSingleProcessMock(t, configPath)
	defer server.Close()

	// Step 3: Write initial config with MAX_TURNS: 1
	writeConfig(t, configPath, server.URL, 1)

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Step 4: Single invocation — agent starts with MAX_TURNS: 1, but
	// after the mock rewrites the config mid-session, the hot-reload
	// mechanism picks up MAX_TURNS: 3, allowing all three turns.
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "process files")
	if err != nil {
		t.Fatalf("expected clean exit after hot-reload, got error: %v\nStderr: %s", err, stderr)
	}

	count := callCount.Load()
	if count != 3 {
		t.Errorf("expected 3 LLM calls (hot-reloaded MAX_TURNS=3), got %d", count)
	}

	out := stripANSI(stdout)
	assertContains(t, out, "Done.")
}
