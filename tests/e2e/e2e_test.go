// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
	cmd := exec.Command(binPath, args...)
	cmd.Dir = projectRoot // Run from project root to ensure default config paths work
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
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
