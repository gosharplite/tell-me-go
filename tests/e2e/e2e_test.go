// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}
	projectRoot = filepath.Dir(filepath.Dir(wd))
	mainPath := filepath.Join(projectRoot, "cmd/tell-me-go/main.go")

	fmt.Printf("Building binary: %s from %s\n", binPath, mainPath)
	build := exec.Command("go", "build", "-o", binPath, mainPath)
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build binary: %v\nOutput: %s\n", err, string(out))
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(tempDir)
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
	cmd.Env = append(cmd.Env, "GEMINI_API_KEY=dummy")

	// If a mock server is spun up, TELL_ME_MOCK_URL might be needed by gemini.go
	// we will inject it where necessary inside the tests.

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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	// 2. Create dummy session files
	outputDir := filepath.Join(homeDir, "output")
	modeDir := filepath.Join(outputDir, "assistant")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}

	histFile := filepath.Join(modeDir, "history.jsonl")
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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	// 2. Create dummy session files including bypass
	outputDir := filepath.Join(homeDir, "output")
	modeDir := filepath.Join(outputDir, "assistant")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}

	histFile := filepath.Join(modeDir, "history.jsonl")
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
func runAgentStep(t *testing.T, dir string, env []string, input string, wantSubstrs []string, args ...string) (string, string) {
	t.Helper()
	cmdArgs := append(args, input)
	stdout, stderr, err := runCommandWithEnvInDir(dir, env, "", cmdArgs...)
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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	// 1. Shared Setup: Create dummy persistent and session files
	modeDir := filepath.Join(homeDir, "output", "assistant")
	if err := os.MkdirAll(modeDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFiles := []string{"history.jsonl", "tokens.log", "commands.log"}
	persistentFiles := map[string]string{
		"safepaths.json": "[]",
		"readpaths.json": "[]",
		"tasks.json":     "[]",
		"bypass.log":     "false",
	}

	for f, content := range persistentFiles {
		_ = os.WriteFile(filepath.Join(modeDir, f), []byte(content), 0644)
	}
	for _, f := range sessionFiles {
		_ = os.WriteFile(filepath.Join(modeDir, f), []byte("session content"), 0644)
	}

	// 2. Shared Setup: Setup mock server
	server, _ := setupProviderMockServer(t, "google", "list_files", map[string]interface{}{"path": "."}, nil)
	defer server.Close()

	configPath := createTempConfig(t, "google", server.URL)
	env = append(env, "TELL_ME_MOCK_URL="+server.URL)

	var stdout, stderr string

	t.Run("ExecuteNewSession", func(t *testing.T) {
		var err error
		stdout, stderr, err = runCommandWithEnv(env, "", "-c", configPath, "-new", "hello persistence")
		if err != nil {
			t.Fatalf("unexpected command failure: %v\nStderr: %s", err, stderr)
		}
	})

	t.Run("VerifyPersistentFiles", func(t *testing.T) {
		for f := range persistentFiles {
			if _, err := os.Stat(filepath.Join(modeDir, f)); os.IsNotExist(err) {
				t.Errorf("Expected persistent file %s to remain. Stderr: %s", f, stderr)
			}
		}
	})

	t.Run("VerifyArchivedFiles", func(t *testing.T) {
		verifyFilesArchived(t, homeDir, sessionFiles, stdout, stderr)
	})
}

func verifyFilesArchived(t *testing.T, homeDir string, sessionFiles []string, stdout, stderr string) {
	t.Helper()
	backupsDir := filepath.Join(homeDir, "output", "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("Expected backup entries in %s. Err: %v. Stderr: %s. Stdout: %s", backupsDir, err, stderr, stdout)
	}

	backupSubDir := filepath.Join(backupsDir, entries[0].Name())
	for _, f := range sessionFiles {
		content, err := os.ReadFile(filepath.Join(backupSubDir, f))
		if err != nil {
			t.Errorf("Failed to read archived file %s: %v", f, err)
			continue
		}
		if string(content) != "session content" {
			t.Errorf("Expected archived session file %s in backup with 'session content', got %q", f, string(content))
		}
	}
}

func TestStdinPiping(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

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
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	providers := []string{"google", "openai", "anthropic"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			server, _ := setupProviderMockServer(t, provider, "list_files", map[string]interface{}{"path": "."}, func(res string) string {
				return "I have listed the files."
			})
			defer server.Close()

			homeDir := t.TempDir()
			configPath := createTempConfig(t, provider, server.URL)
			env := []string{
				"TELL_ME_HOME=" + homeDir,
				"TELL_ME_MOCK_URL=" + server.URL,
			}

			stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "list the files")
			if err != nil {
				t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
			}

			out := stripANSI(stdout)
			errOut := stripANSI(stderr)

			if !strings.Contains(errOut, "[Tool Action] list_files") {
				t.Errorf("Expected tool engine log in stderr, got: %q", errOut)
			}
			if !strings.Contains(out, "I have listed the files.") {
				t.Errorf("Expected final answer in stdout, got: %q", out)
			}
		})
	}
}

func TestWriteFileConfirmation(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	providers := []string{"google", "openai", "anthropic"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			server, _ := setupProviderMockServer(t, provider, "write_file", map[string]interface{}{"filepath": "test.txt", "content": "hello world"}, func(result string) string {
				return "File written."
			})
			defer server.Close()

			homeDir := t.TempDir()
			configPath := createTempConfig(t, provider, server.URL)
			env := []string{
				"TELL_ME_HOME=" + homeDir,
				"TELL_ME_MOCK_ANSWER=y",
				"TELL_ME_MOCK_URL=" + server.URL,
			}

			// 2. Run CLI and Verification
			runAgentStep(t, homeDir, env, "write a file", []string{"Do you approve all?", "File written."}, "-c", configPath)

			// Verify file actually written
			content, err := os.ReadFile(filepath.Join(homeDir, "test.txt"))
			if err != nil {
				t.Errorf("File was not written: %v", err)
			} else if string(content) != "hello world" {
				t.Errorf("File content mismatch. Expected 'hello world', got %q", string(content))
			}
		})
	}
}

func TestWriteFileDenial(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	providers := []string{"google", "openai", "anthropic"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			server, _ := setupProviderMockServer(t, provider, "write_file", map[string]interface{}{"filepath": "denied.txt", "content": "should not exist"}, func(result string) string {
				if strings.Contains(result, "User explicitly denied this action.") {
					return "Model acknowledges denial."
				}
				return "Error: Denial failed."
			})
			defer server.Close()

			homeDir := t.TempDir()
			configPath := createTempConfig(t, provider, server.URL)
			env := []string{
				"TELL_ME_HOME=" + homeDir,
				"TELL_ME_MOCK_ANSWER=n",
				"TELL_ME_MOCK_URL=" + server.URL,
			}

			// 2. Run CLI and Verification
			runAgentStep(t, homeDir, env, "write a file", []string{"Model acknowledges denial."}, "-c", configPath)

			// Verify file NOT written
			if _, err := os.Stat(filepath.Join(homeDir, "denied.txt")); !os.IsNotExist(err) {
				t.Errorf("File 'denied.txt' should not have been created")
			}
		})
	}
}

func TestSecurityGate(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	providers := []string{"google", "openai", "anthropic"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			homeDir := t.TempDir()

			// Use helper to encapsulate mock server logic
			server, receivedResponse := setupProviderMockServer(t, provider, "read_file", map[string]interface{}{
				"filepath": "/etc/passwd",
			}, nil)
			defer server.Close()

			configPath := createTempConfig(t, provider, server.URL)
			env := []string{
				"TELL_ME_HOME=" + homeDir,
				"TELL_ME_MOCK_URL=" + server.URL,
			}

			_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "read /etc/passwd")
			if err == nil {
				t.Fatalf("CLI succeeded when it should have failed due to security violation")
			}
			if !strings.Contains(string(stderr), "security violation") && !strings.Contains(*receivedResponse, "security violation") && !strings.Contains(string(stderr), "security policy") && !strings.Contains(*receivedResponse, "security policy") {
				t.Errorf("Expected security error in stderr or response. stderr: %s, response: %q", stderr, *receivedResponse)
			}
		})
	}
}

func TestSymlinkAttack(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	homeDir := t.TempDir()
	evilLink := filepath.Join(homeDir, "evil_link")
	if err := os.Symlink("/etc/passwd", evilLink); err != nil {
		t.Fatal(err)
	}

	provider := "google"
	// Use helper to encapsulate mock server logic
	server, receivedResponse := setupProviderMockServer(t, provider, "read_file", map[string]interface{}{
		"filepath": "evil_link",
	}, nil)
	defer server.Close()

	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	_, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "read evil_link")
	if err == nil {
		t.Fatalf("CLI succeeded when it should have failed due to symlink attack")
	}

	if !strings.Contains(string(stderr), "security violation") && !strings.Contains(*receivedResponse, "security violation") && !strings.Contains(string(stderr), "security policy") && !strings.Contains(*receivedResponse, "security policy") {
		t.Errorf("Expected security error for symlink attack. stderr: %s, response: %q", stderr, *receivedResponse)
	}
}

func TestManageTasks(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}
	provider := "google"
	server, _ := setupProviderMockServer(t, provider, "manage_tasks", map[string]interface{}{
		"action":  "add",
		"content": "End-to-End Test Task",
	}, nil)
	defer server.Close()

	homeDir := t.TempDir()
	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// 2. Run CLI and Verification
	runAgentStep(t, homeDir, env, "add a task", []string{"Done."}, "-c", configPath)

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
		defer func() { _ = db.Close() }()
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM tasks WHERE content LIKE '%End-to-End Test Task%'").Scan(&count)
		if err != nil || count == 0 {
			t.Errorf("Task not found in sqlite database")
		}
	}
}

func createTempConfig(t *testing.T, providerType, mockURL string) string {
	content := fmt.Sprintf(`
MODE: "assistant"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "%s"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gpt-4"
`, providerType, mockURL)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}
	return path
}

func createTempConfigWithMode(t *testing.T, providerType, mockURL, mode string) string {
	content := fmt.Sprintf(`
MODE: "%s"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "%s"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gpt-4"
`, mode, providerType, mockURL)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}
	return path
}

func setupSimpleTextMockServer(t *testing.T, provider string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate endpoint mapping based on provider
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
		// Return a simple text response for any request
		resp := createTextResponse(provider, "Hello, I'm the model.")
		_, _ = fmt.Fprint(w, resp)
	}))
}

func setupProviderMockServer(t *testing.T, provider string, toolName string, toolArgs map[string]interface{}, onToolResponse func(string) string) (*httptest.Server, *string) {
	t.Helper()
	receivedResponse := new(string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate endpoint mapping based on provider
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

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}

		t.Logf("MOCK_SERVER_REQUEST [%s]: %s\n", provider, string(bodyBytes))

		w.Header().Set("Content-Type", "application/json")
		toolResult := extractToolResult(bodyBytes, provider)

		if toolResult == nil {
			// Turn 1: Return the requested tool call
			resStr := createToolCallResponse(provider, toolName, toolArgs)
			t.Logf("MOCK_SERVER_RESPONSE [%s]: %s\n", provider, resStr)
			_, _ = fmt.Fprint(w, resStr)
		} else {
			// Turn 2: Capture the response from the agent and return a final message
			*receivedResponse = *toolResult
			finalText := "Done."
			if onToolResponse != nil {
				finalText = onToolResponse(*toolResult)
			}
			resStr := createTextResponse(provider, finalText)
			t.Logf("MOCK_SERVER_RESPONSE [%s]: %s\n", provider, resStr)
			_, _ = fmt.Fprint(w, resStr)
		}
	}))
	return server, receivedResponse
}

func extractToolResult(reqBody []byte, provider string) *string {
	var body map[string]interface{}
	if err := json.Unmarshal(reqBody, &body); err != nil {
		return nil
	}

	switch provider {
	case "google":
		return extractGoogleToolResult(body)
	case "openai":
		return extractOpenAIToolResult(body)
	case "anthropic":
		return extractAnthropicToolResult(body)
	}
	return nil
}

func extractGoogleToolResult(body map[string]interface{}) *string {
	contents, ok := body["contents"].([]interface{})
	if !ok || len(contents) == 0 {
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
	functionResponse, ok := part["functionResponse"].(map[string]interface{})
	if !ok {
		return nil
	}
	responseMap, ok := functionResponse["response"].(map[string]interface{})
	if !ok {
		return nil
	}
	result, _ := responseMap["result"].(string)
	return &result
}

func extractOpenAIToolResult(body map[string]interface{}) *string {
	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return nil
	}
	lastMsg, ok := messages[len(messages)-1].(map[string]interface{})
	if !ok {
		return nil
	}
	if role, _ := lastMsg["role"].(string); role != "tool" {
		return nil
	}
	content, _ := lastMsg["content"].(string)
	return &content
}

func extractAnthropicToolResult(body map[string]interface{}) *string {
	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return nil
	}
	lastMsg, ok := messages[len(messages)-1].(map[string]interface{})
	if !ok {
		return nil
	}
	contents, ok := lastMsg["content"].([]interface{})
	if !ok || len(contents) == 0 {
		return nil
	}
	for _, c := range contents {
		block, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if typ, _ := block["type"].(string); typ == "tool_result" {
			contentStr, _ := block["content"].(string)
			return &contentStr
		}
	}
	return nil
}

func createToolCallResponse(provider string, name string, args map[string]interface{}) string {
	argsBytes, _ := json.Marshal(args)
	argsStr := string(argsBytes) // For OpenAI

	switch provider {
	case "google":
		return fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_123","name":%q,"args":%s}}]}}]}`, name, string(argsBytes))
	case "openai":
		resp := map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"message": map[string]interface{}{
						"role": "assistant",
						"tool_calls": []interface{}{
							map[string]interface{}{
								"id":   "call_123",
								"type": "function",
								"function": map[string]interface{}{
									"name":      name,
									"arguments": argsStr,
								},
							},
						},
					},
				},
			},
		}
		b, _ := json.Marshal(resp)
		return string(b)
	case "anthropic":
		resp := map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "tool_123",
					"name":  name,
					"input": args,
				},
			},
		}
		b, _ := json.Marshal(resp)
		return string(b)
	}
	return ""
}

func createTextResponse(provider string, text string) string {
	switch provider {
	case "google":
		return fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":%q}]}}]}`, text)
	case "openai":
		return fmt.Sprintf(`{"choices": [{"message": {"role": "assistant", "content": %q}}]}`, text)
	case "anthropic":
		return fmt.Sprintf(`{"role": "assistant", "content": [{"type": "text", "text": %q}]}`, text)
	}
	return ""
}
