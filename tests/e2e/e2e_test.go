// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"context"
	"fmt"
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = projectRoot
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

	histFile := filepath.Join(outputDir, "last-vertex.json")
	logFile := filepath.Join(outputDir, "last-vertex.json.log")

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
