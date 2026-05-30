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
	"strings"
	"sync/atomic"
	"testing"
)

// setupHotReloadMock returns an httptest.Server implementing a multi-turn
// Google-format mock and an atomic call counter. The server returns:
//
//	Turn 0 → list_files tool call
//	Turn 1 → read_files tool call
//	Turn 2+→ text response "Done."
//
// The turn index is derived from the Google API contents array length:
//
//	idx := (len(contents) - 1) / 2
func setupHotReloadMock(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

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

		// Google provider: each turn adds 2 messages (model + user/tool).
		// contents[0]=user prompt, contents[1]=model tool call,
		// contents[2]=tool response, etc.
		idx := (len(contents) - 1) / 2
		if idx < 0 {
			idx = 0
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

// TestConfigHotReload_MaxTurns verifies that MAX_TURNS is reloaded
// from the config file between invocations when the session directory
// is reused. The test runs the binary twice against the same home
// directory, overwriting the config with a higher MAX_TURNS value
// for the second run.
func TestConfigHotReload_MaxTurns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()

	// Step 1: Setup mock server with call counter
	server, callCount := setupHotReloadMock(t)
	defer server.Close()

	// Step 2: Write config A with MAX_TURNS: 1
	configPath := filepath.Join(homeDir, "config.yaml")
	writeConfig(t, configPath, server.URL, 1)

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Step 3: Invocation 1 — agent makes 2 LLM calls, then MAX_TURNS=1 stops it.
	// Turn 0: list_files (executed). Turn 1: read_files (LLM called but tool
	// execution blocked by Dispatcher's >= check, as maxToolTurns=1).
	// The agent exits non-zero when MAX_TURNS is exhausted; this is expected.
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "process files")
	if err != nil {
		out := stripANSI(stderr)
		if !strings.Contains(out, "maximum tool execution turns") {
			t.Fatalf("invocation 1 failed with unexpected error: %v\nStderr: %s", err, stderr)
		}
	}

	count1 := callCount.Load()
	if count1 != 2 {
		t.Errorf("invocation 1: expected 2 LLM calls (MAX_TURNS=1), got %d", count1)
	}

	// Step 4: Overwrite config B with MAX_TURNS: 3 (same path, update ModTime)
	writeConfig(t, configPath, server.URL, 3)

	// Step 5: Invocation 2 — agent continues session, now allows more calls.
	// The existing session has 2 prior LLM responses (list_files executed,
	// read_files blocked at Dispatcher). With MAX_TURNS=3, the agent makes
	// 1 more call and receives the final "Done." text response.
	stdout2, stderr2, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "continue processing")
	if err != nil {
		t.Fatalf("invocation 2 failed: %v\nStderr: %s", err, stderr2)
	}

	count2 := callCount.Load()
	if count2 != 3 {
		t.Errorf("invocation 2: expected 3 total LLM calls (MAX_TURNS=3), got %d", count2)
	}

	out2 := stripANSI(stdout2)
	assertContains(t, out2, "Done.")
}
