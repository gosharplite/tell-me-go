// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var helperPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "tmgo-test-*")
	if err != nil {
		log.Fatalf("failed to create temp dir: %v", err)
	}

	// Build the helper binary
	target := filepath.Join(tmpDir, "helper")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", target, "../../infrastructure/testing/bin/helper/main.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		log.Fatalf("failed to build test helper: %v\n%s", err, string(out))
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		os.RemoveAll(tmpDir)
		log.Fatalf("failed to get absolute path for helper: %v", err)
	}

	helperPath = absPath

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}
