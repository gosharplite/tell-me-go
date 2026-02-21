// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

var binPath string
var projectRoot string

func TestMain(m *testing.M) {
	// Build the binary once for all E2E tests
	tempDir, err := os.MkdirTemp("", "tell-me-go-e2e")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	binPath = filepath.Join(tempDir, "tell-me-go")

	// Get absolute path to project root
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get working directory: %v\n", err)
		os.RemoveAll(tempDir)
		os.Exit(1)
	}
	projectRoot = filepath.Dir(filepath.Dir(wd))
	mainPath := filepath.Join(projectRoot, "cmd/tell-me-go/main.go")

	fmt.Printf("Building binary: %s from %s\n", binPath, mainPath)
	build := exec.Command("go", "build", "-o", binPath, mainPath)
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build binary: %v\nOutput: %s\n", err, string(out))
		os.RemoveAll(tempDir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(tempDir)
	os.Exit(code)
}

// Helper to strip ANSI escape codes
func stripANSI(str string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return re.ReplaceAllString(str, "")
}

func runCommand(args ...string) (string, string, error) {
	return runCommandWithEnv(nil, "", args...)
}

func runCommandWithEnv(env []string, stdin string, args ...string) (string, string, error) {
	return runCommandWithEnvInDir(projectRoot, env, stdin, args...)
}

func runCommandWithEnvInDir(dir string, env []string, stdin string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Ensure absolute path to default config
	configFlag := fmt.Sprintf("-c=%s", filepath.Join(projectRoot, "configs/assistant.yaml"))

	// Prepend config flag to ensure it's always set to a valid location
	finalArgs := append([]string{configFlag}, args...)

	cmd := exec.CommandContext(ctx, binPath, finalArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestSessionArchiving(t *testing.T) {
	// 1. Setup isolated home directory
	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	// 2. Create dummy session files
	outputDir := filepath.Join(homeDir, "output")
	modeDir := filepath.Join(outputDir, "assistant")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}

	histFile := filepath.Join(modeDir, "history.json")
	logFile := filepath.Join(modeDir, "tokens.log")

	if err := os.WriteFile(histFile, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logFile, []byte("log data"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Run with -new flag (and a dummy prompt to trigger the logic)
	// We expect it to fail on API call but archive the files first
	_, _, _ = runCommandWithEnv(env, "", "-new", "hello")

	// 4. Verify archive exists
	backupsDir := filepath.Join(outputDir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("Expected backup directory to be created and contain entries, got error: %v", err)
	}

	// Verify original files are archived (check backup content)
	backupSubDir := filepath.Join(backupsDir, entries[0].Name())
	archivedLog, err := os.ReadFile(filepath.Join(backupSubDir, "tokens.log"))
	if err != nil || string(archivedLog) != "log data" {
		t.Errorf("Expected archived log to contain 'log data', got %q (err: %v)", string(archivedLog), err)
	}
}

func TestBypassArchiving(t *testing.T) {
	// 1. Setup isolated home directory
	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	// 2. Create dummy session files including bypass
	outputDir := filepath.Join(homeDir, "output")
	modeDir := filepath.Join(outputDir, "assistant")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}

	histFile := filepath.Join(modeDir, "history.json")
	bypassFile := filepath.Join(modeDir, "bypass.log")

	if err := os.WriteFile(histFile, []byte("[]"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bypassFile, []byte("true"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Run with -new flag
	_, _, _ = runCommandWithEnv(env, "", "-new", "hello")

	// 4. Verify bypass file STILL exists in output (not archived)
	if _, err := os.Stat(bypassFile); os.IsNotExist(err) {
		t.Errorf("Expected bypass file to remain in output directory, but it was moved or deleted")
	}

	// 5. Verify it's NOT in the backup
	backupsDir := filepath.Join(outputDir, "backups")
	entries, _ := os.ReadDir(backupsDir)
	if len(entries) > 0 {
		backupPath := filepath.Join(backupsDir, entries[0].Name(), "bypass.log")
		if _, err := os.Stat(backupPath); err == nil {
			t.Errorf("Expected bypass file NOT to be archived, but found it in %s", backupPath)
		}
	}
}

// Helper to drive agent conversation and assertions
func runAgentStep(t *testing.T, dir string, env []string, input string, wantSubstrs []string) (string, string) {
	t.Helper()
	stdout, stderr, err := runCommandWithEnvInDir(dir, env, "", input)
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}
	out := stripANSI(stdout)
	errOut := stripANSI(stderr)
	combined := out + errOut
	for _, s := range wantSubstrs {
		if !strings.Contains(combined, s) {
			t.Errorf("expected output to contain %q, got: %q", s, combined)
		}
	}
	return out, errOut
}

func TestEnvironmentPersistence(t *testing.T) {
	// 1. Setup isolated home directory
	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	// 2. Create dummy persistent and session files
	outputDir := filepath.Join(homeDir, "output")
	modeDir := filepath.Join(outputDir, "assistant")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFiles := []string{"history.json", "tokens.log", "commands.log"}
	persistentFiles := []string{"safepaths.json", "scratchpad.md", "tasks.json", "bypass.log", "tellmego.db"}

	for _, f := range sessionFiles {
		_ = os.WriteFile(filepath.Join(modeDir, f), []byte("session content"), 0644)
	}
	for _, f := range persistentFiles {
		_ = os.WriteFile(filepath.Join(modeDir, f), []byte("persistent content"), 0644)
	}

	// 3. Run with -new flag
	_, _, _ = runCommandWithEnv(env, "", "-new", "hello persistence")

	// 4. Verify persistent files STILL exist in output
	for _, f := range persistentFiles {
		if _, err := os.Stat(filepath.Join(modeDir, f)); os.IsNotExist(err) {
			t.Errorf("Expected persistent file %s to remain, but it was moved or deleted", f)
		}
	}

	// 5. Verify session files are archived (check backup content)
	backupsDir := filepath.Join(outputDir, "backups")
	entries, _ := os.ReadDir(backupsDir)
	if len(entries) == 0 {
		t.Fatalf("Expected backup entries")
	}

	backupSubDir := filepath.Join(backupsDir, entries[0].Name())
	for _, f := range sessionFiles {
		content, _ := os.ReadFile(filepath.Join(backupSubDir, f))
		if string(content) != "session content" {
			t.Errorf("Expected archived session file %s in backup with 'session content'", f)
		}
	}
}

func TestStdinPiping(t *testing.T) {
	// Use a fake config to avoid real API attempts but verify prompt capture
	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	stdinContent := "This is from stdin"
	// We check if the stderr shows "Input captured" which is a log in main.go
	_, stderr, _ := runCommandWithEnv(env, stdinContent, "Prompt from arg")

	out := stripANSI(stderr)
	if !strings.Contains(out, "Input captured") {
		t.Errorf("Expected 'Input captured' log in stderr, got: %q", out)
	}
}

func TestVersionFlag(t *testing.T) {
	stdout, _, err := runCommand("-v")
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	out := stripANSI(stdout)
	if !strings.Contains(out, "tell-me-go version") {
		t.Errorf("Expected version string, got: %q", out)
	}
}

func TestHelpOutput(t *testing.T) {
	// Running with no args should show help/usage
	stdout, stderr, err := runCommand()

	if err == nil {
		t.Error("Expected error when running without arguments, got nil")
	}

	combined := stripANSI(stdout + stderr)
	if !strings.Contains(combined, "Usage:") {
		t.Errorf("Expected usage instructions, got: %q", combined)
	}
}

func TestToolOrchestrationLoop(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var body struct {
			Contents []interface{} `json:"contents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			return
		}

		// State-based detection
		isToolResponse := false
		if len(body.Contents) > 0 {
			lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
			if parts, ok := lastTurn["parts"].([]interface{}); ok && len(parts) > 0 {
				if _, ok := parts[0].(map[string]interface{})["functionResponse"]; ok {
					isToolResponse = true
				}
			}
		}

		response := `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"list_files","args":{"path":"."}}}]}}]}`
		if isToolResponse {
			response = `{"candidates":[{"content":{"role":"model","parts":[{"text":"I have listed the files."}]}}]}`
		}
		fmt.Fprint(w, response)
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "list the files")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	out := stripANSI(stdout)
	errOut := stripANSI(stderr)

	if !strings.Contains(errOut, "Calling: list_files") {
		t.Errorf("Expected tool engine log in stderr, got: %q", errOut)
	}
	if !strings.Contains(out, "I have listed the files.") {
		t.Errorf("Expected final answer in stdout, got: %q", out)
	}
}

func TestWriteFileConfirmation(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return
			}

			w.Header().Set("Content-Type", "application/json")

			// State-based detection
			isToolResponse := false
			if len(body.Contents) > 0 {
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				if parts, ok := lastTurn["parts"].([]interface{}); ok && len(parts) > 0 {
					if _, ok := parts[0].(map[string]interface{})["functionResponse"]; ok {
						isToolResponse = true
					}
				}
			}

			if !isToolResponse {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "write_file",
									"args": {"filepath": "test.txt", "content": "hello world"}
								}
							}]
						}
					}]
				}`)
			} else {
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "File written."}]}}]}`)
			}
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_MOCK_ANSWER=y",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI and Verification
	runAgentStep(t, homeDir, env, "write a file", []string{"[CONFIRMATION REQUIRED]", "File written."})

	// Verify file actually written
	content, err := os.ReadFile(filepath.Join(homeDir, "test.txt"))
	if err != nil {
		t.Errorf("File was not written: %v", err)
	} else if string(content) != "hello world" {
		t.Errorf("File content mismatch. Expected 'hello world', got %q", string(content))
	}
}

func TestWriteFileDenial(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return
			}

			w.Header().Set("Content-Type", "application/json")

			// State-based detection
			var toolResponse map[string]interface{}
			if len(body.Contents) > 0 {
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				if parts, ok := lastTurn["parts"].([]interface{}); ok && len(parts) > 0 {
					if resp, ok := parts[0].(map[string]interface{})["functionResponse"]; ok {
						toolResponse = resp.(map[string]interface{})
					}
				}
			}

			if toolResponse == nil {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "write_file",
									"args": {"filepath": "denied.txt", "content": "should not exist"}
								}
							}]
						}
					}]
				}`)
			} else {
				// We check the tool response
				result := toolResponse["response"].(map[string]interface{})["result"].(string)

				if strings.Contains(result, "The user explicitly denied this action.") {
					fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Model acknowledges denial."}]}}]}`)
				} else {
					fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Error: Denial failed."}]}}]}`)
				}
			}
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_MOCK_ANSWER=n",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI and Verification
	runAgentStep(t, homeDir, env, "write a file", []string{"Model acknowledges denial."})

	// Verify file NOT written
	if _, err := os.Stat(filepath.Join(homeDir, "denied.txt")); !os.IsNotExist(err) {
		t.Errorf("File 'denied.txt' should not have been created")
	}
}

func TestSecurityGate(t *testing.T) {
	homeDir := t.TempDir()

	// Use helper to encapsulate mock server logic
	server, receivedResponse := setupToolMockServer(t, "read_file", map[string]interface{}{
		"filepath": "/etc/passwd",
	})
	defer server.Close()

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "read /etc/passwd")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	if !strings.Contains(*receivedResponse, "security violation") {
		t.Errorf("Expected security violation error to be sent back to model, got: %q", *receivedResponse)
	}
}

func TestSymlinkAttack(t *testing.T) {
	homeDir := t.TempDir()
	evilLink := filepath.Join(homeDir, "evil_link")
	if err := os.Symlink("/etc/passwd", evilLink); err != nil {
		t.Fatal(err)
	}

	// Use helper to encapsulate mock server logic
	server, receivedResponse := setupToolMockServer(t, "read_file", map[string]interface{}{
		"filepath": "evil_link",
	})
	defer server.Close()

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "read evil_link")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	if !strings.Contains(*receivedResponse, "security violation") {
		t.Errorf("Expected security violation for symlink attack, got: %q", *receivedResponse)
	}
}

func TestManageTasks(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return
			}

			w.Header().Set("Content-Type", "application/json")

			// State-based detection
			isToolResponse := false
			if len(body.Contents) > 0 {
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				if parts, ok := lastTurn["parts"].([]interface{}); ok && len(parts) > 0 {
					if _, ok := parts[0].(map[string]interface{})["functionResponse"]; ok {
						isToolResponse = true
					}
				}
			}

			if !isToolResponse {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "manage_tasks",
									"args": {"action": "add", "content": "End-to-End Test Task"}
								}
							}]
						}
					}]
				}`)
			} else {
				// Turn 2
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Task added."}]}}]}`)
			}
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI and Verification
	runAgentStep(t, homeDir, env, "add a task", []string{"Task added."})

	// Check if database exists and has content
	dbFile := filepath.Join(homeDir, "output", "assistant", "tellmego.db")
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		t.Fatalf("SQLite database missing at %s", dbFile)
	}

	importSQL := true
	if importSQL {
		db, err := sql.Open("sqlite", dbFile)
		if err != nil {
			t.Fatalf("Failed to open sqlite db: %v", err)
		}
		defer db.Close()
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM tasks WHERE content LIKE '%End-to-End Test Task%'").Scan(&count)
		if err != nil || count == 0 {
			t.Errorf("Task not found in sqlite database")
		}
	}
}

func TestManageScratchpad(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return
			}

			w.Header().Set("Content-Type", "application/json")

			// State-based detection
			isToolResponse := false
			if len(body.Contents) > 0 {
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				if parts, ok := lastTurn["parts"].([]interface{}); ok && len(parts) > 0 {
					if _, ok := parts[0].(map[string]interface{})["functionResponse"]; ok {
						isToolResponse = true
					}
				}
			}

			if !isToolResponse {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "manage_scratchpad",
									"args": {"action": "write", "content": "# E2E Scratchpad"}
								}
							}]
						}
					}]
				}`)
			} else {
				// Turn 2
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Scratchpad updated."}]}}]}`)
			}
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
		"TELL_ME_NO_STREAM=true",
	}

	// 2. Run CLI and Verification
	runAgentStep(t, homeDir, env, "update scratchpad", []string{"Scratchpad updated."})

	// Check if database exists and has content
	dbFile := filepath.Join(homeDir, "output", "assistant", "tellmego.db")
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		t.Fatalf("SQLite database missing at %s", dbFile)
	}

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	defer db.Close()
	var contentStr string
	err = db.QueryRow("SELECT content FROM scratchpad WHERE id = 1").Scan(&contentStr)
	if err != nil || !strings.Contains(contentStr, "# E2E Scratchpad") {
		t.Errorf("Scratchpad mismatch. Got: %v (err: %v)", contentStr, err)
	}
}

// setupToolMockServer creates a mock HTTP server that simulates a tool call from the model
// and captures the agent's response.
func setupToolMockServer(t *testing.T, initialCall string, initialArgs map[string]interface{}) (*httptest.Server, *string) {
	t.Helper()
	receivedResponse := new(string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "generateContent") {
			return
		}

		var body struct {
			Contents []interface{} `json:"contents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		toolResponse := extractToolResponse(body.Contents)

		if toolResponse == nil {
			// Turn 1: Return the requested tool call
			resp := createToolCallResponse(initialCall, initialArgs)
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Errorf("failed to encode mock response: %v", err)
			}
		} else {
			// Turn 2: Capture the response from the agent and return a final message
			captureAgentResult(toolResponse, receivedResponse)
			fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Done."}]}}]}`)
		}
	}))
	return server, receivedResponse
}

func extractToolResponse(contents []interface{}) map[string]interface{} {
	if len(contents) == 0 {
		return nil
	}
	lastTurn, ok := contents[len(contents)-1].(map[string]interface{})
	if !ok {
		return nil
	}
	parts, ok := lastTurn["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		return nil
	}
	part, ok := parts[0].(map[string]interface{})
	if !ok {
		return nil
	}
	resp, ok := part["functionResponse"].(map[string]interface{})
	if !ok {
		return nil
	}
	return resp
}

func captureAgentResult(toolResponse map[string]interface{}, out *string) {
	if response, ok := toolResponse["response"].(map[string]interface{}); ok {
		if result, ok := response["result"].(string); ok {
			*out = result
		}
	}
}

func createToolCallResponse(name string, args map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role": "model",
					"parts": []interface{}{
						map[string]interface{}{
							"functionCall": map[string]interface{}{
								"name": name,
								"args": args,
							},
						},
					},
				},
			},
		},
	}
}
