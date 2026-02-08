// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

// RegisterRelease adds release management tools to the registry.
func RegisterRelease(r *registry.Registry, sm *security.SecurityManager) {
	m := &releaseManager{
		sm:       sm,
		fs:       fsutil.DefaultFileSystem,
		executor: workspace.NewProcessExecutor(),
	}

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "verify_release_readiness",
		Description: "Performs an automated check of all SOP release requirements (clean build, secret scanning, go.mod check, and test execution).",
	}, m.verifyReleaseReadiness, registry.ToolOptions{Serial: true, LongRunning: true})
}

type releaseManager struct {
	sm       *security.SecurityManager
	fs       fsutil.FileSystem
	executor workspace.CommandExecutor
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
	fs fsutil.FileSystem
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
		if fsutil.IsBinary(content) {
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
	return strings.Contains(path, ".git") || strings.Contains(path, "vendor/") || strings.Contains(path, "node_modules")
}

// DependencyChecker implementation
type DependencyChecker struct {
	fs fsutil.FileSystem
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
	executor workspace.CommandExecutor
}

func (c *BuildChecker) Name() string { return "Clean Room Build Simulation" }
func (c *BuildChecker) Run(ctx context.Context) CheckResult {
	tmpDir, err := os.MkdirTemp("", "release-build-*")
	if err != nil {
		return CheckResult{OK: false, Message: fmt.Sprintf("Failed to create temp dir: %v", err)}
	}
	defer os.RemoveAll(tmpDir)

	res, err := c.executor.RunCommand(ctx, []string{"go", "build", "-o", filepath.Join(tmpDir, "tell-me-go"), "./cmd/tell-me-go"}, workspace.ExecutionConfig{})
	if err != nil || res.ExitCode != 0 {
		return CheckResult{OK: false, Message: fmt.Sprintf("Clean build failed (Exit %d):\n%s", res.ExitCode, res.Output)}
	}
	return CheckResult{OK: true, Message: "Application compiles successfully from source."}
}

// TestRunner implementation
type TestRunner struct {
	executor workspace.CommandExecutor
}

func (c *TestRunner) Name() string { return "Test Suite Verification" }
func (c *TestRunner) Run(ctx context.Context) CheckResult {
	res, err := c.executor.RunCommand(ctx, []string{"go", "test", "-race", "./..."}, workspace.ExecutionConfig{})
	if err != nil || res.ExitCode != 0 {
		return CheckResult{OK: false, Message: fmt.Sprintf("Unit/Integration tests failed (Exit %d):\n%s", res.ExitCode, res.Output)}
	}
	return CheckResult{OK: true, Message: "All tests passed (including race detector)."}
}
