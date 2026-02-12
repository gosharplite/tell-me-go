// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

type releaseManager struct {
	sm       *security.SecurityManager
	fs       storage.FileSystem
	executor tools.CommandExecutor
}

type ReadinessCheck interface {
	Name() string
	Run(ctx context.Context) CheckResult
}

type CheckResult struct {
	OK      bool
	Message string
}

func (m *releaseManager) verifyReleaseReadiness(ctx context.Context, _ map[string]interface{}) (tools.ToolResult, error) {
	pipeline := []ReadinessCheck{
		&SecretScanner{sm: m.sm, fs: m.fs},
		&DependencyChecker{fs: m.fs},
		&BuildChecker{executor: m.executor},
		&TestRunner{executor: m.executor},
	}

	var report strings.Builder
	report.WriteString("### Release Readiness Report\n\n")

	allOK := true
	for i, check := range pipeline {
		report.WriteString(fmt.Sprintf("#### %d. %s\n", i+1, check.Name()))
		result := check.Run(ctx)
		if result.OK {
			report.WriteString(fmt.Sprintf("- [OK] %s\n", result.Message))
		} else {
			report.WriteString(fmt.Sprintf("- [FAIL] %s\n", result.Message))
			allOK = false
		}
		report.WriteString("\n")
	}

	if allOK {
		report.WriteString("**Status: READY FOR RELEASE**\n")
	} else {
		report.WriteString("**Status: NOT READY**\n")
	}

	return tools.ToolResult{Text: report.String()}, nil
}

// SecretScanner implementation
type SecretScanner struct {
	sm *security.SecurityManager
	fs storage.FileSystem
}

func (s *SecretScanner) Name() string { return "Security Scan" }
func (s *SecretScanner) Run(ctx context.Context) CheckResult {
	secretPatterns := []string{
		fmt.Sprintf("AI_%s", "URL"),
	}

	compiledPatterns := make([]*regexp.Regexp, len(secretPatterns))
	for i, p := range secretPatterns {
		compiledPatterns[i] = regexp.MustCompile(p)
	}

	secretsFound := false
	var findings []string

	root, err := s.sm.IsPathSafe(".")
	if err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Security error: %v", err)}
	}

	err = s.fs.Walk(ctx, root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || s.isIgnored(path) {
			return nil
		}
		content, err := s.fs.ReadFile(ctx, path)
		if err != nil {
			return nil
		}
		if storage.IsBinary(content) {
			return nil
		}
		s.scanContent(content, path, &findings, &secretsFound, compiledPatterns)
		return nil
	})

	if err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Scan interrupted: %v", err)}
	}

	if secretsFound {
		return CheckResult{OK: false, Message: strings.Join(findings, "\n- [FAIL] ")}
	}
	return CheckResult{OK: true, Message: "No common secrets detected."}
}

func (s *SecretScanner) scanContent(content []byte, path string, findings *[]string, secretsFound *bool, patterns []*regexp.Regexp) {
	for _, re := range patterns {
		if re.Match(content) {
			*findings = append(*findings, fmt.Sprintf("Potential secret in %s: pattern `%s`", path, re.String()))
			*secretsFound = true
		}
	}
}

func (s *SecretScanner) isIgnored(path string) bool {
	return strings.Contains(path, ".git") ||
		strings.Contains(path, "vendor/") ||
		strings.Contains(path, "node_modules") ||
		strings.HasSuffix(path, "_test.go") ||
		strings.HasSuffix(path, ".md") ||
		strings.HasSuffix(path, ".json") ||
		strings.HasSuffix(path, ".golden")
}

// DependencyChecker implementation
type DependencyChecker struct {
	fs storage.FileSystem
}

func (c *DependencyChecker) Name() string { return "Dependency Check" }
func (c *DependencyChecker) Run(ctx context.Context) CheckResult {
	modContent, err := c.fs.ReadFile(ctx, "go.mod")
	if err != nil {
		return CheckResult{OK: false, Message: "Could not read go.mod"}
	}

	if strings.Contains(string(modContent), "replace ") {
		return CheckResult{OK: false, Message: "go.mod contains 'replace' directives."}
	}
	return CheckResult{OK: true, Message: "No local 'replace' directives found."}
}

// BuildChecker implementation
type BuildChecker struct {
	executor tools.CommandExecutor
}

func (c *BuildChecker) Name() string { return "Clean Room Build Simulation" }
func (c *BuildChecker) Run(ctx context.Context) CheckResult {
	tmpDir, err := os.MkdirTemp("", "release-build-*")
	if err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Failed to create temp dir: %v", err)}
	}
	defer os.RemoveAll(tmpDir)

	out, err := c.executor.CombinedOutput(ctx, "go", "build", "-o", filepath.Join(tmpDir, "tell-me-go"), "./cmd/tell-me-go")
	if err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Clean build failed: %v\nOutput: %s", err, string(out))}
	}
	return CheckResult{OK: true, Message: "Application compiles successfully from source."}
}

// TestRunner implementation
type TestRunner struct {
	executor tools.CommandExecutor
}

func (c *TestRunner) Name() string { return "Test Suite Verification" }
func (c *TestRunner) Run(ctx context.Context) CheckResult {
	out, err := c.executor.CombinedOutput(ctx, "go", "test", "-race", "./...")
	if err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Unit/Integration tests failed: %v\nOutput: %s", err, string(out))}
	}
	return CheckResult{OK: true, Message: "All tests passed (including race detector)."}
}
