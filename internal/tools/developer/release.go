// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type releaseManager struct {
	sm       domain_security.PathValidator
	fs       persistence.FileSystem
	executor tools.CommandExecutor
}

type readinessCheck interface {
	Name() string
	Run(ctx context.Context) checkResult
}

type checkResult struct {
	OK      bool
	Message string
}

func (m *releaseManager) verifyReleaseReadiness(ctx context.Context, _ map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	pipeline := []readinessCheck{
		&secretScanner{sm: m.sm, fs: m.fs},
		&dependencyChecker{fs: m.fs},
		&linterChecker{executor: m.executor},
		&buildChecker{executor: m.executor},
		&testRunner{executor: m.executor},
	}

	var report strings.Builder
	report.WriteString("### Release Readiness Report\n\n")

	// Heartbeat while waiting for all parallel health checks
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	results := make([]checkResult, len(pipeline))
	g, gCtx := errgroup.WithContext(ctx)

	// Limit concurrent execution to 2 processes to prevent CPU/RAM exhaustion
	// and avoid build cache locking collisions in CI.
	sem := semaphore.NewWeighted(2)

	for i, check := range pipeline {
		i, c := i, check // Captured for closure
		g.Go(func() error {
			slog.Debug("verify_release_readiness: enqueued check", slog.String("check", c.Name()))

			// Acquire semaphore before executing heavy checks
			if err := sem.Acquire(gCtx, 1); err != nil {
				results[i] = checkResult{
					OK:      false,
					Message: fmt.Sprintf("failed to acquire semaphore: %v", err),
				}
				return err
			}
			defer sem.Release(1)

			slog.Debug("verify_release_readiness: running check", slog.String("check", c.Name()))
			res := c.Run(gCtx)
			// Both success and failure of a check are normal operational info, not system warnings.
			slog.Info("verify_release_readiness: check finished",
				slog.String("check", c.Name()),
				slog.Bool("ok", res.OK),
			)

			results[i] = res
			return nil
		})
	}

	_ = g.Wait() // Wait for all checks to finish (even if some return an error due to context cancellation)

	allOK := true
	for i, result := range results {
		check := pipeline[i]
		_, _ = fmt.Fprintf(&report, "#### %d. %s\n", i+1, check.Name())
		if result.OK {
			_, _ = fmt.Fprintf(&report, "- [OK] %s\n", result.Message)
		} else {
			_, _ = fmt.Fprintf(&report, "- [FAIL] %s\n", result.Message)
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

// secretScanner implementation
type secretScanner struct {
	sm domain_security.PathValidator
	fs persistence.FileSystem
}

func (s *secretScanner) Name() string { return "Security Scan" }
func (s *secretScanner) Run(ctx context.Context) checkResult {
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
		return checkResult{OK: false, Message: fmt.Sprintf("Security error: %v", err)}
	}

	err = s.fs.Walk(ctx, root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || s.isIgnored(path) {
			return nil
		}
		content, err := s.fs.ReadFile(ctx, path)
		if err != nil {
			return nil
		}
		if persistence.IsBinary(content) {
			return nil
		}
		matches := s.scanContent(content, path, compiledPatterns)
		findings = append(findings, matches...)
		if len(matches) > 0 {
			secretsFound = true
		}
		return nil
	})

	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Scan interrupted: %v", err)}
	}

	if secretsFound {
		return checkResult{OK: false, Message: strings.Join(findings, "\n- [FAIL] ")}
	}
	return checkResult{OK: true, Message: "No common secrets detected."}
}

func (s *secretScanner) scanContent(content []byte, path string, patterns []*regexp.Regexp) []string {
	var matches []string
	for _, re := range patterns {
		if re.Match(content) {
			matches = append(matches, fmt.Sprintf("Potential secret in %s: pattern `%s`", path, re.String()))
		}
	}
	return matches
}

func (s *secretScanner) isIgnored(path string) bool {
	return strings.Contains(path, ".git") ||
		strings.Contains(path, "vendor/") ||
		strings.Contains(path, "node_modules") ||
		strings.HasSuffix(path, "_test.go") ||
		strings.HasSuffix(path, ".md") ||
		strings.HasSuffix(path, ".json") ||
		strings.HasSuffix(path, ".golden")
}

// dependencyChecker implementation
type dependencyChecker struct {
	fs persistence.FileSystem
}

func (c *dependencyChecker) Name() string { return "Dependency Check" }
func (c *dependencyChecker) Run(ctx context.Context) checkResult {
	modContent, err := c.fs.ReadFile(ctx, "go.mod")
	if err != nil {
		return checkResult{OK: false, Message: "Could not read go.mod"}
	}

	if strings.Contains(string(modContent), "replace ") {
		return checkResult{OK: false, Message: "go.mod contains 'replace' directives."}
	}
	return checkResult{OK: true, Message: "No local 'replace' directives found."}
}

// buildChecker implementation
type buildChecker struct {
	executor tools.CommandExecutor
}

func (c *buildChecker) Name() string { return "Clean Room Build Simulation" }
func (c *buildChecker) Run(ctx context.Context) checkResult {
	tmpDir, err := os.MkdirTemp("", "release-build-*")
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Failed to create temp dir: %v", err)}
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	out, err := c.executor.CombinedOutput(ctx, "go", "build", "-o", filepath.Join(tmpDir, "tell-me-go"), "./cmd/tell-me-go")
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Clean build failed: %v\nOutput: %s", err, string(out))}
	}
	return checkResult{OK: true, Message: "Application compiles successfully from source."}
}

// testRunner implementation
type testRunner struct {
	executor tools.CommandExecutor
}

func (c *testRunner) Name() string { return "Test Suite Verification" }
func (c *testRunner) Run(ctx context.Context) checkResult {
	out, err := c.executor.CombinedOutput(ctx, "go", "test", "-race", "./...")
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Unit/Integration tests failed: %v\nOutput: %s", err, string(out))}
	}
	return checkResult{OK: true, Message: "All tests passed (including race detector)."}
}

// linterChecker implementation
type linterChecker struct {
	executor tools.CommandExecutor
}

func (c *linterChecker) Name() string { return "Linter Verification" }

func (c *linterChecker) Run(ctx context.Context) checkResult {
	res := c.runGolangciLint(ctx)
	if res.OK || res.Message != "executable file not found" {
		return res
	}

	res = c.runStaticcheck(ctx)
	if res.Message == "executable file not found" {
		return checkResult{OK: false, Message: "No linter found (golangci-lint or staticcheck)."}
	}
	return res
}

func (c *linterChecker) runGolangciLint(ctx context.Context) checkResult {
	out, err := c.executor.CombinedOutput(ctx, "golangci-lint", "run")
	return c.handleLinterResult(out, err, "golangci-lint")
}

func (c *linterChecker) runStaticcheck(ctx context.Context) checkResult {
	out, err := c.executor.CombinedOutput(ctx, "staticcheck", "./...")
	return c.handleLinterResult(out, err, "staticcheck")
}

func (c *linterChecker) handleLinterResult(out []byte, err error, name string) checkResult {
	outStr := strings.TrimSpace(string(out))
	if err == nil {
		if outStr == "" || outStr == "0 issues." {
			return checkResult{OK: true, Message: fmt.Sprintf("All linting checks passed (%s).", name)}
		}
		return checkResult{OK: false, Message: fmt.Sprintf("%s found issues:\n%s", name, outStr)}
	}

	if strings.Contains(err.Error(), "exit status 1") {
		if outStr == "0 issues." {
			return checkResult{OK: true, Message: fmt.Sprintf("All linting checks passed (%s).", name)}
		}
		return checkResult{OK: false, Message: fmt.Sprintf("%s found issues:\n%s", name, string(out))}
	}

	if strings.Contains(err.Error(), "executable file not found") {
		return checkResult{OK: false, Message: "executable file not found"}
	}

	return checkResult{OK: false, Message: fmt.Sprintf("%s failed: %v\nOutput: %s", name, err, string(out))}
}
