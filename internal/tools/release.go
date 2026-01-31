// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
)

// RegisterReleaseTools adds release management tools to the registry.
func RegisterReleaseTools(r *Registry, sm *SecurityManager) {
	m := &releaseManager{sm: sm}

	r.Register(&types.ToolDeclaration{
		Name:        "verify_release_readiness",
		Description: "Performs an automated check of all SOP release requirements (clean build, secret scanning, go.mod check, and test execution).",
	}, m.verifyReleaseReadiness)
}

type releaseManager struct {
	sm *SecurityManager
}

func (m *releaseManager) verifyReleaseReadiness(ctx context.Context, _ map[string]interface{}) (types.ToolResult, error) {
	var report strings.Builder
	report.WriteString("### Release Readiness Report\n\n")

	// 1. Secret Scanning
	report.WriteString("#### 1. Security Scan\n")
	secretsFound := false
	secretPatterns := []string{
		`"private_key"`,
		`"client_email"`,
		`AI_URL`,
	}
	
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || strings.Contains(path, ".git") || strings.Contains(path, "vendor/") {
			return nil
		}
		content, _ := os.ReadFile(path)
		for _, pattern := range secretPatterns {
			if matched, _ := regexp.MatchString(pattern, string(content)); matched {
				report.WriteString(fmt.Sprintf("- [FAIL] Potential secret found in %s: pattern `%s`\n", path, pattern))
				secretsFound = true
			}
		}
		return nil
	})
	if err == nil && !secretsFound {
		report.WriteString("- [OK] No common secrets detected.\n")
	}

	// 2. go.mod Check
	report.WriteString("\n#### 2. Dependency Check\n")
	modContent, err := os.ReadFile("go.mod")
	if err != nil {
		report.WriteString("- [FAIL] Could not read go.mod\n")
	} else if strings.Contains(string(modContent), "replace ") {
		report.WriteString("- [FAIL] go.mod contains 'replace' directives. These must be removed for public release.\n")
	} else {
		report.WriteString("- [OK] No local 'replace' directives found.\n")
	}

	// 3. Clean Room Build
	report.WriteString("\n#### 3. Clean Room Build Simulation\n")
	tmpDir, _ := os.MkdirTemp("", "release-build-*")
	defer os.RemoveAll(tmpDir)

	// Build the current package into the temp dir
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(tmpDir, "tell-me-go"), "./cmd/tell-me-go")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		report.WriteString(fmt.Sprintf("- [FAIL] Clean build failed:\n```\n%s\n```\n", string(out)))
	} else {
		report.WriteString("- [OK] Application compiles successfully from source.\n")
	}

	// 4. Test Execution
	report.WriteString("\n#### 4. Test Suite Verification\n")
	testCmd := exec.CommandContext(ctx, "go", "test", "-race", "./...")
	if out, err := testCmd.CombinedOutput(); err != nil {
		report.WriteString(fmt.Sprintf("- [FAIL] Unit/Integration tests failed:\n```\n%s\n```\n", string(out)))
	} else {
		report.WriteString("- [OK] All tests passed (including race detector).\n")
	}

	return types.ToolResult{Text: report.String()}, nil
}
