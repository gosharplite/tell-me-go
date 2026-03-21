// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestApplication_RapidConsecutiveActions_NoDeadlock(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Strict timeout: If locks block forever, the test fails explicitly in 3 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 2. Hermetic Environment
	homeDir := t.TempDir()

	// Setup mock provider server to simulate LLM responses triggering tools.
	// We'll return MULTIPLE parallel tool calls per turn to stress-test the RWMutex locks 
	// added to the persistence layer.
	provider := "google"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := parseMockRequest(t, r)

		isToolResult := isToolResultTurn(t, body)
		resp := generateMockResponse(t, isToolResult, provider)
		_, _ = fmt.Fprint(w, resp)
	}))
	defer server.Close()

	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
		"TELL_ME_NO_STREAM=true",
		"TELL_ME_MOCK_ANSWER=y", // Approve all tool calls
	}

	// 3. Define a storm of actions (prompts)
	actions := []string{
		"Update config and tasks 1",
		"Update config and tasks 2",
		"Update config and tasks 3",
		"List all tasks and check config",
	}

	done := make(chan struct{})

	// 4. Execute rapidly
	go func() {
		for _, action := range actions {
			// Run the built binary via the runCommandWithEnv helper from e2e_test.go
			_, _, err := runCommandWithEnv(env, "", "-c", configPath, action)
			if err != nil {
				t.Logf("Action %q finished with error: %v", action, err)
			}
		}
		close(done)
	}()

	// 5. Liveness Check
	select {
	case <-done:
		// PASS: All actions completed within the timeout.
	case <-ctx.Done():
		t.Fatal("DEADLOCK DETECTED: Application hung during rapid user actions. Likely a sync.RWMutex contention issue.")
	}
}

func parseMockRequest(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	return body
}

func isToolResultTurn(t *testing.T, body map[string]interface{}) bool {
	t.Helper()
	contents, ok := body["contents"].([]interface{})
	if !ok || len(contents) == 0 {
		return false
	}
	lastTurn, ok := contents[len(contents)-1].(map[string]interface{})
	if !ok {
		return false
	}
	parts, ok := lastTurn["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		return false
	}
	part, ok := parts[0].(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = part["functionResponse"]
	return ok
}

func generateMockResponse(t *testing.T, isToolResult bool, provider string) string {
	t.Helper()
	if isToolResult {
		return createTextResponse(provider, "Storm turn processed successfully.")
	}
	// Return multiple tool calls in parallel.
	return `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [
					{"functionCall": {"name": "manage_tasks", "args": {"action": "add", "content": "Parallel Task A"}}},
					{"functionCall": {"name": "manage_tasks", "args": {"action": "add", "content": "Parallel Task B"}}},
					{"functionCall": {"name": "manage_config", "args": {"action": "set", "key": "c1", "value": "v1"}}},
					{"functionCall": {"name": "manage_config", "args": {"action": "set", "key": "c2", "value": "v2"}}}
				]
			}
		}]
	}`
}
