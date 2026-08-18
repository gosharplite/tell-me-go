// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// stdioServerPath is the absolute path to the built stdioserver fixture
// binary, populated by TestMain. Every integration test spawns it as a real
// child process via StdioClient.
var stdioServerPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "tmgo-mcp-stdio-test-*")
	if err != nil {
		log.Fatalf("failed to create temp dir: %v", err)
	}

	// Build the fixture binary. Relative to the mcp package directory (go
	// test runs with the package dir as the working directory).
	target := filepath.Join(tmpDir, "stdioserver")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", target, "./testdata/stdioserver")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		log.Fatalf("failed to build stdio test server: %v\n%s", err, string(out))
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		log.Fatalf("failed to get absolute path for stdio test server: %v", err)
	}

	stdioServerPath = absPath

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}
