// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvCommand(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, "config.yaml")
	configContent := `
MODE: "assistant"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "google"
    API_KEY: "secret-key"
    MODEL: "gemini-pro"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	env := []string{"TELL_ME_HOME=" + homeDir}
	stdout, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "env")
	if err != nil {
		t.Fatalf("env command failed: %v\nStderr: %s", err, stderr)
	}

	out := stripANSI(stdout)
	if !strings.Contains(out, "MODE: assistant") {
		t.Errorf("Expected output to contain 'MODE: assistant', got: %q", out)
	}
	if !strings.Contains(out, "API_KEY: '********'") {
		t.Errorf("Expected API_KEY to be masked with single quotes, got: %q", out)
	}
	if strings.Contains(out, "secret-key") {
		t.Errorf("Expected 'secret-key' to NOT be in the output")
	}
}

func TestBrowseCommand_NoTTY(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	homeDir := t.TempDir()
	configPath := createTempConfig(t, "google", "http://localhost")
	env := []string{"TELL_ME_HOME=" + homeDir}

	// browse command should fail when run without a TTY (which is the case in exec.Command)
	_, stderr, err := runCommandWithEnv(env, "", "-c="+configPath, "browse")
	if err == nil {
		t.Fatal("Expected browse command to fail without TTY, but it succeeded")
	}

	errOut := stripANSI(stderr)
	if !strings.Contains(errOut, "requires an interactive TTY") {
		t.Errorf("Expected error about requiring TTY, got: %q", errOut)
	}
}

func TestConfigAutoDiscovery(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Create a temporary working directory
	workDir := t.TempDir()

	// 2. Create assistant.yaml in that directory
	configContent := `
MODE: "assistant"
SELECTED_PROVIDER: "mock"
PROVIDERS:
  mock:
    TYPE: "google"
    API_KEY: "secret-key"
    MODEL: "auto-discovered-model"
`
	if err := os.WriteFile(filepath.Join(workDir, "assistant.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// 3. Run 'env' command in that directory without -c flag
	env := []string{"TELL_ME_HOME=" + workDir}
	stdout, stderr, err := runCommandWithEnvInDirEx(workDir, env, "", false, "env")
	if err != nil {
		t.Fatalf("env command failed: %v\nStderr: %s", err, stderr)
	}

	// 4. Verify it used the auto-discovered config
	out := stripANSI(stdout)
	if !strings.Contains(out, "MODEL: auto-discovered-model") {
		t.Errorf("Expected auto-discovered config to be used, got: %q", out)
	}
}
