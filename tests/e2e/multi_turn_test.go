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
	"testing"
)

func TestMultiTurnToolOrchestration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	provider := "google"
	var turnCount int

	// Define a sequence of model responses
	// 1. Call list_files
	// 2. Based on result, call read_file
	// 3. Final answer
	responses := []func(reqBody []byte) string{
		func(reqBody []byte) string {
			return createToolCallResponse(provider, "list_files", map[string]interface{}{"path": "."})
		},
		func(reqBody []byte) string {
			// Verify previous tool result was sent back
			if !strings.Contains(string(reqBody), "test.txt") {
				t.Errorf("Expected request to contain 'test.txt' from list_files result, got: %s", string(reqBody))
			}
			return createToolCallResponse(provider, "read_file", map[string]interface{}{"filepath": "test.txt"})
		},
		func(reqBody []byte) string {
			// Verify read_file result was sent back
			if !strings.Contains(string(reqBody), "file content") {
				t.Errorf("Expected request to contain 'file content' from read_file result, got: %s", string(reqBody))
			}
			return createTextResponse(provider, "The file content is 'file content'.")
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		// We need to determine if it's a new turn or a continuation of a tool call
		// In Google provider, tool results are sent in 'parts' as 'functionResponse'

		// Increment turn only when we receive a response from the agent (either tool result or new prompt)
		// Actually, let's just use the length of the 'contents' array in the request body to decide which response to give.
		var body map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &body)
		contents, _ := body["contents"].([]interface{})

		// For the first message, len(contents) is 1.
		// After first tool call, the agent sends back the history including the tool call and tool response.
		// Google provider history management:
		// [USER: prompt]
		// [MODEL: tool call]
		// [USER: tool response]
		// So for the second turn, the model sees 3 messages.
		// For the third turn, it sees 5 messages.

		idx := (len(contents) - 1) / 2
		if idx >= len(responses) {
			idx = len(responses) - 1
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, responses[idx](bodyBytes)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
		turnCount++
	}))
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// Setup: create a dummy file for the tool to "read"
	if err := os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "process the files")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	out := stripANSI(stdout)
	if !strings.Contains(out, "The file content is 'file content'.") {
		t.Errorf("Expected final answer in stdout, got: %q", out)
	}

	errOut := stripANSI(stderr)
	if !strings.Contains(errOut, "[Tool Action] list_files") {
		t.Errorf("Expected list_files tool action log")
	}
	if !strings.Contains(errOut, "[Tool Action] read_file") {
		t.Errorf("Expected read_file tool action log")
	}
}

func TestToolExecutionInHistory(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	provider := "google"
	server, _ := setupProviderMockServer(t, provider, "list_files", map[string]interface{}{"path": "."}, func(res string) string {
		return "Files are listed."
	})
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// 1. Run the command that triggers a tool call
	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "list files")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 2. Verify turn.log contains tool execution details
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "-t")
	if err != nil {
		t.Fatalf("CLI -t failed: %v\nStderr: %s", err, stderr)
	}

	out := stripANSI(stdout)
	if !strings.Contains(out, "Turn 1") {
		t.Errorf("Expected turns.log to contain Turn 1 header, got: %q", out)
	}
	if !strings.Contains(out, "Turn 2") {
		t.Errorf("Expected turns.log to contain Turn 2 header, got: %q", out)
	}
	if !strings.Contains(out, "Ready") {
		t.Errorf("Expected turns.log to contain 'Ready' footer, got: %q", out)
	}
}
