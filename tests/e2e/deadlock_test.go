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
		bodyBytes, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")

		var body map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &body)

		isToolResult := false
		// Check if it's a tool result turn
		if contents, ok := body["contents"].([]interface{}); ok && len(contents) > 0 {
			lastTurn := contents[len(contents)-1].(map[string]interface{})
			if parts, ok := lastTurn["parts"].([]interface{}); ok && len(parts) > 0 {
				part := parts[0].(map[string]interface{})
				if _, ok := part["functionResponse"]; ok {
					isToolResult = true
				}
			}
		}

		if isToolResult {
			// Final response after tool execution
			_, _ = fmt.Fprint(w, createTextResponse(provider, "Storm turn processed successfully."))
		} else {
			// Return multiple tool calls in parallel. 
			// manage_tasks and manage_config are registered as non-serial,
			// so they will execute concurrently in the ToolExecutor.
			resp := `{
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
			_, _ = fmt.Fprint(w, resp)
		}
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
