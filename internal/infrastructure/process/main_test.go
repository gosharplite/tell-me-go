// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package process

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
	tmpDir, err := os.MkdirTemp("", "tmgo-process-test-*")
	if err != nil {
		log.Fatalf("failed to create temp dir: %v", err)
	}

	// Build the shared helper binary (same testdata helper the exec and
	// workspace suites use — INTENTIONAL_NON_FIXES.md, structural concerns).
	target := filepath.Join(tmpDir, "helper")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}

	// Relative path to helper source from internal/infrastructure/process
	cmd := exec.Command("go", "build", "-o", target, "../../infrastructure/testing/testdata/helper/main.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		log.Fatalf("failed to build test helper: %v\n%s", err, string(out))
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		log.Fatalf("failed to get absolute path for helper: %v", err)
	}

	helperPath = absPath

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}
