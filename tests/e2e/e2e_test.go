// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
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
	wd, _ := os.Getwd()
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
	configFlag := fmt.Sprintf("-c=%s", filepath.Join(projectRoot, "configs/vertex.yaml"))

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
	os.MkdirAll(outputDir, 0755)

	histFile := filepath.Join(outputDir, "vertex-history.json")
	logFile := filepath.Join(outputDir, "vertex-tokens.log")

	os.WriteFile(histFile, []byte("[]"), 0644)
	os.WriteFile(logFile, []byte("log data"), 0644)

	// 3. Run with -new flag (and a dummy prompt to trigger the logic)
	// We expect it to fail on API call but archive the files first
	_, _, _ = runCommandWithEnv(env, "", "-new", "hello")

	// 4. Verify archive exists
	backupsDir := filepath.Join(outputDir, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("Expected backup directory to be created and contain entries, got error: %v", err)
	}

	// Verify original files are gone (moved)
	if _, err := os.Stat(histFile); !os.IsNotExist(err) {
		t.Errorf("Expected original history file to be moved, but it still exists")
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
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "list_files",
									"args": {"path": "."}
								}
							}]
						}
					}]
				}`)
			} else {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{"text": "I have listed the files."}]
						}
					}]
				}`)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
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
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
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
	}

	// 2. Run CLI
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "write a file")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 3. Verification
	out := stripANSI(stdout)
	errOut := stripANSI(stderr)

	t.Logf("Stderr: %s", errOut)

	if !strings.Contains(errOut, "[CONFIRMATION REQUIRED]") {
		t.Errorf("Expected confirmation prompt in stderr, got: %q", errOut)
	}
	if !strings.Contains(out, "File written.") {
		t.Errorf("Expected success message, got: %q", out)
	}

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
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
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
				// We check the tool response in Turn 2
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				parts := lastTurn["parts"].([]interface{})
				resp := parts[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
				result := resp["response"].(map[string]interface{})["result"].(string)

				if result == "Action denied by user." {
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
	}

	// 2. Run CLI
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "write a file")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 3. Verification
	out := stripANSI(stdout)
	if !strings.Contains(out, "Model acknowledges denial.") {
		t.Errorf("Expected model to acknowledge denial, got: %q", out)
	}

	// Verify file NOT written
	if _, err := os.Stat(filepath.Join(homeDir, "denied.txt")); !os.IsNotExist(err) {
		t.Errorf("File 'denied.txt' should not have been created")
	}
}

func TestSecurityGate(t *testing.T) {
	// 1. Setup Mock Server that forces a security violation
	var receivedResponse string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
				// Turn 1: Return a malicious function call
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "read_file",
									"args": {"filepath": "/etc/passwd"}
								}
							}]
						}
					}]
				}`)
			} else {
				// Turn 2: Capture the error response sent by the agent
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				parts := lastTurn["parts"].([]interface{})
				resp := parts[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
				receivedResponse = resp["response"].(map[string]interface{})["result"].(string)

				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Security error caught."}]}}]}`)
			}
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
	}

	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "read /etc/passwd")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	if !strings.Contains(receivedResponse, "security violation") {
		t.Errorf("Expected security violation error to be sent back to model, got: %q", receivedResponse)
	}
}

func TestSymlinkAttack(t *testing.T) {
	// 1. Setup Mock Server
	var receivedResponse string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
				fmt.Fprint(w, `{
					"candidates": [{
						"content": {
							"role": "model",
							"parts": [{
								"functionCall": {
									"name": "read_file",
									"args": {"filepath": "evil_link"}
								}
							}]
						}
					}]
				}`)
			} else {
				lastTurn := body.Contents[len(body.Contents)-1].(map[string]interface{})
				parts := lastTurn["parts"].([]interface{})
				resp := parts[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
				receivedResponse = resp["response"].(map[string]interface{})["result"].(string)
				fmt.Fprint(w, `{"candidates": [{"content": {"role": "model", "parts": [{"text": "Done."}]}}]}`)
			}
			return
		}
	}))
	defer server.Close()

	homeDir := t.TempDir()
	// Create a symlink in the homeDir
	evilLink := filepath.Join(homeDir, "evil_link")
	os.Symlink("/etc/passwd", evilLink)

	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL + "/",
	}

	_, _, err := runCommandWithEnvInDir(homeDir, env, "", "read evil_link")
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}

	if !strings.Contains(receivedResponse, "security violation") {
		t.Errorf("Expected security violation for symlink attack, got: %q", receivedResponse)
	}
}

func TestManageTasks(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
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
	}

	// 2. Run CLI
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "add a task")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 3. Verification
	out := stripANSI(stdout)
	if !strings.Contains(out, "Task added.") {
		t.Errorf("Expected success message, got: %q", out)
	}

	// Check if file exists and has content
	taskFile := filepath.Join(homeDir, "output", "tasks_vertex.json")
	if _, err := os.Stat(taskFile); os.IsNotExist(err) {
		t.Fatalf("Tasks file was not created at %s", taskFile)
	}

	content, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("Failed to read tasks file: %v", err)
	}

	if !strings.Contains(string(content), "End-to-End Test Task") {
		t.Errorf("Tasks file does not contain expected content. Got: %s", string(content))
	}
}

func TestManageScratchpad(t *testing.T) {
	// 1. Setup Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "generateContent") {
			var body struct {
				Contents []interface{} `json:"contents"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			if len(body.Contents) <= 1 {
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
	}

	// 2. Run CLI
	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "update scratchpad")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 3. Verification
	out := stripANSI(stdout)
	if !strings.Contains(out, "Scratchpad updated.") {
		t.Errorf("Expected success message, got: %q", out)
	}

	// Check if file exists and has content
	scratchpadFile := filepath.Join(homeDir, "output", "scratchpad_vertex.md")
	if _, err := os.Stat(scratchpadFile); os.IsNotExist(err) {
		t.Fatalf("Scratchpad file was not created at %s", scratchpadFile)
	}

	content, err := os.ReadFile(scratchpadFile)
	if err != nil {
		t.Fatalf("Failed to read scratchpad file: %v", err)
	}

	if !strings.Contains(string(content), "# E2E Scratchpad") {
		t.Errorf("Scratchpad file does not contain expected content. Got: %s", string(content))
	}
}
