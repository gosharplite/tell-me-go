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

// buildMultiTurnResponses returns a 3-step mock response sequence for
// the multi-turn orchestration test, plus a collector for any validation
// errors detected inside closures (which run on the mock server goroutine
// and must not call t.Errorf directly).
func buildMultiTurnResponses(t *testing.T) ([]func(reqBody []byte) string, *[]string) {
	t.Helper()
	provider := "google"
	var validationErrors []string

	responses := []func(reqBody []byte) string{
		// Step 1: call list_files
		func(reqBody []byte) string {
			return createToolCallResponse(provider, "list_files", map[string]interface{}{
				"path":   ".",
				"reason": "E2E multi-turn step 1: list workspace files",
			})
		},
		// Step 2: verify list_files result was sent back, then call read_files
		func(reqBody []byte) string {
			if !strings.Contains(string(reqBody), "test.txt") {
				validationErrors = append(validationErrors,
					fmt.Sprintf("expected request to contain 'test.txt' from list_files result, got: %s", string(reqBody)))
			}
			return createToolCallResponse(provider, "read_files", map[string]interface{}{
				"filepaths": []interface{}{"test.txt"},
				"reason":    "E2E multi-turn step 2: read discovered file",
			})
		},
		// Step 3: verify read_files result was sent back, then final answer
		func(reqBody []byte) string {
			if !strings.Contains(string(reqBody), "file content") {
				validationErrors = append(validationErrors,
					fmt.Sprintf("expected request to contain 'file content' from read_file result, got: %s", string(reqBody)))
			}
			return createTextResponse(provider, "The file content is 'file content'.")
		},
	}
	return responses, &validationErrors
}

// setupMultiTurnE2E creates an httptest server that dispatches requests
// through the provided response sequence, a temp home directory, a
// provider config pointing at the server, and a fixture file.
func setupMultiTurnE2E(t *testing.T, provider string, responses []func(reqBody []byte) string) (*httptest.Server, string, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)

		var body map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &body)
		contents, _ := body["contents"].([]interface{})

		// Google provider: each turn adds 2 messages (model + user/tool).
		// contents[0]=user prompt, contents[1]=model tool call,
		// contents[2]=tool response, etc.
		idx := (len(contents) - 1) / 2
		if idx >= len(responses) {
			idx = len(responses) - 1
		}

		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, responses[idx](bodyBytes)); err != nil {
			t.Errorf("failed to write mock response: %v", err)
		}
	}))

	homeDir := t.TempDir()
	configPath := createTempConfig(t, provider, server.URL)

	// Fixture file for the tool to "read"
	if err := os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	return server, homeDir, configPath
}

func TestMultiTurnToolOrchestration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	provider := "google"
	responses, validationErrs := buildMultiTurnResponses(t)
	server, homeDir, configPath := setupMultiTurnE2E(t, provider, responses)
	defer server.Close()

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "process the files")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// Report validation errors captured during mock response closures
	for _, e := range *validationErrs {
		t.Error(e)
	}

	out := stripANSI(stdout)
	assertContains(t, out, "The file content is 'file content'.")

	errOut := stripANSI(stderr)
	assertContains(t, errOut, "[Tool Action] list_files")
	assertContains(t, errOut, "[Tool Action] read_files")
}

func TestToolExecutionInHistory(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	provider := "google"
	server, _ := setupProviderMockServer(t, provider, "list_files", map[string]interface{}{"path": ".", "reason": "E2E verification of tool execution in history"}, func(res string) string {
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
