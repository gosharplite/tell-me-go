// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecurity_CWDWriteAuthorization(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Setup workspace
	workspace := t.TempDir()
	homeDir := t.TempDir()
	
	// Ensure we are testing in a subdirectory to avoid any root-level weirdness
	projectDir := filepath.Join(workspace, "my-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 2. Setup mock server
	// The agent will try to create a directory 'test_dir' in the CWD
	provider := "google"
	server, receivedResponse := setupProviderMockServer(t, provider, "create_directory", map[string]interface{}{
		"path":   "test_dir",
		"reason": "E2E verification of CWD safety",
	}, nil)
	defer server.Close()

	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_ANSWER=y", // Auto-approve
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	// 3. Run command with CWD set to projectDir
	stdout, stderr, err := runCommandWithEnvInDir(projectDir, env, "", "-c", configPath, "create a directory named test_dir")
	
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s\nStdout: %s", err, stderr, stdout)
	}

	// 4. Verify results
	combined := stripANSI(stdout + stderr)
	
	// Check if the tool execution was blocked
	if strings.Contains(combined, "Action blocked by the system sandbox security policy") {
		t.Errorf("Security regression: CWD write was blocked.\nOutput: %s", combined)
	}

	// Verify the response from the agent contains success
	if !strings.Contains(*receivedResponse, "Directory created successfully") {
		t.Errorf("Expected tool success message in agent response, got: %q", *receivedResponse)
	}

	// Verify directory actually exists on disk in the projectDir
	if _, err := os.Stat(filepath.Join(projectDir, "test_dir")); os.IsNotExist(err) {
		t.Errorf("Directory 'test_dir' was not created in the CWD")
	}
}

func TestSecurity_DetailedErrorMessage(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// This test verifies that if a security block occurs, the detailed error is visible.
	homeDir := t.TempDir()
	provider := "google"

	var targetPath string
	var expectedMention string

	if runtime.GOOS == "windows" {
		sysRoot := os.Getenv("SystemRoot")
		if sysRoot == "" {
			t.Fatal("SystemRoot environment variable is not set on Windows")
		}
		absSysRoot, _ := filepath.Abs(filepath.Clean(sysRoot))
		targetPath = filepath.Join(absSysRoot, "System32", "drivers", "etc", "hosts")
		expectedMention = absSysRoot
	} else {
		targetPath = "/etc/passwd"
		expectedMention = "/etc"
	}

	// Try to read a sensitive path which should definitely be blocked
	server, receivedResponse := setupProviderMockServer(t, provider, "read_file", map[string]interface{}{
		"filepath": targetPath,
	}, nil)
	defer server.Close()

	configPath := createTempConfig(t, provider, server.URL)
	env := []string{
		"TELL_ME_HOME=" + homeDir,
		"TELL_ME_MOCK_URL=" + server.URL,
	}

	_, stderr, _ := runCommandWithEnv(env, "", "-c", configPath, "read "+targetPath)

	errOut := stripANSI(stderr)

	// Check for the specific error message instead of the generic one
	if !strings.Contains(errOut, "access to system directory") && !strings.Contains(*receivedResponse, "access to system directory") {
		t.Errorf("Expected detailed security error message (access to system directory), but got generic or none.\nStderr: %s\nResponse: %s", errOut, *receivedResponse)
	}

	if !strings.Contains(errOut, expectedMention) && !strings.Contains(*receivedResponse, expectedMention) {
		t.Errorf("Expected blocked path '%s' to be mentioned in error message.\nStderr: %s\nResponse: %s", expectedMention, errOut, *receivedResponse)
	}
}
