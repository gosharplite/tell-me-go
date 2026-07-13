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
	// Build the helper binary to a stable cache directory so that
	// subsequent test runs skip the go build step when the source
	// hasn't changed. This avoids ~1.5s of linker overhead per run.
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Fatalf("failed to get user cache dir: %v", err)
	}

	helperDir := filepath.Join(cacheDir, "tell-me-go-test-helper")
	if err := os.MkdirAll(helperDir, 0755); err != nil {
		log.Fatalf("failed to create helper cache dir: %v", err)
	}

	target := filepath.Join(helperDir, "helper")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}

	srcFile := "../../infrastructure/testing/testdata/helper/main.go"

	// Only rebuild if the source is newer than the cached binary.
	srcInfo, srcErr := os.Stat(srcFile)
	binInfo, binErr := os.Stat(target)
	if binErr != nil || srcErr != nil || srcInfo.ModTime().After(binInfo.ModTime()) {
		cmd := exec.Command("go", "build", "-o", target, srcFile)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("failed to build test helper: %v\n%s", err, string(out))
		}
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		log.Fatalf("failed to get absolute path for helper: %v", err)
	}

	helperPath = absPath

	os.Exit(m.Run())
}
