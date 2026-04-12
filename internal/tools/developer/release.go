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
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
)

type releaseGoRunner interface {
	RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	RunLinter(ctx context.Context) (string, string, error)
	RunTests(ctx context.Context, path string) ([]byte, error)
	BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error)
}

type releaseManager struct {
	sm           domain_security.PathValidator
	fs           persistence.FileSystem
	runner       releaseGoRunner
	archVerifier tools.ToolFunc
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
	root, err := m.sm.IsPathSafe(".")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("security error: %w", err)
	}

	pipeline := []readinessCheck{
		&secretScanner{root: root, fs: m.fs},
		&dependencyChecker{root: root, fs: m.fs},
		&linterChecker{runner: m.runner},
		&architectureChecker{verifier: m.archVerifier},
		&buildChecker{runner: m.runner},
		&testRunner{runner: m.runner},
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

	// Limit concurrent execution to prevent CPU/RAM exhaustion
	// and avoid build cache locking collisions in CI.
	sem := semaphore.NewWeighted(m.getParallelism())

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
	root string
	fs   persistence.FileSystem
}

func (s *secretScanner) Name() string { return "Security Scan" }
func (s *secretScanner) Run(ctx context.Context) checkResult {
	secretPatterns := []string{
		`sk-[a-zA-Z0-9]{32,}`,                                 // OpenAI/Generic
		`ant-api-key-v1-[a-zA-Z0-9_-]{95,}`,                  // Anthropic
		`AIza[0-9A-Za-z-_]{35}`,                               // Google AI
		`AKIA[0-9A-Z]{16}`,                                    // AWS Access Key
		`(?i)(AI|OPENAI|ANTHROPIC|GEMINI|AWS)_(API_)?KEY`,      // Environment Keys
		`https?://[a-zA-Z0-9]+:[a-zA-Z0-9]+@[a-zA-Z0-9.-]+`, // URLs with Credentials
	}

	compiledPatterns := make([]*regexp.Regexp, len(secretPatterns))
	for i, p := range secretPatterns {
		compiledPatterns[i] = regexp.MustCompile(p)
	}

	secretsFound := false
	var findings []string

	err := s.fs.Walk(ctx, s.root, func(path string, info os.FileInfo, err error) error {
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
		if m := re.Find(content); m != nil {
			secret := string(m)
			masked := secret
			if len(secret) > 8 {
				masked = fmt.Sprintf("%s...%s", secret[:4], secret[len(secret)-4:])
			} else {
				masked = "****"
			}
			matches = append(matches, fmt.Sprintf("Potential secret in %s: pattern `%s` (found `%s`)", path, re.String(), masked))
		}
	}
	return matches
}

func (s *secretScanner) isIgnored(path string) bool {
	p := filepath.ToSlash(filepath.Clean(path))
	// Check for common ignored directories in any part of the path
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == ".git" || part == "vendor" || part == "node_modules" {
			return true
		}
	}

	return strings.HasSuffix(p, "_test.go") ||
		strings.HasSuffix(p, ".md") ||
		strings.HasSuffix(p, ".json") ||
		strings.HasSuffix(p, ".golden")
}

// dependencyChecker implementation
type dependencyChecker struct {
	root string
	fs   persistence.FileSystem
}

func (c *dependencyChecker) Name() string { return "Dependency Check" }
func (c *dependencyChecker) Run(ctx context.Context) checkResult {
	modPath := "go.mod"
	if c.root != "" {
		modPath = filepath.Join(c.root, "go.mod")
	}

	modContent, err := c.fs.ReadFile(ctx, modPath)
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Could not read go.mod at %s", modPath)}
	}

	if strings.Contains(string(modContent), "replace ") {
		return checkResult{OK: false, Message: "go.mod contains 'replace' directives."}
	}
	return checkResult{OK: true, Message: "No local 'replace' directives found."}
}

// buildChecker implementation
type buildChecker struct {
	runner releaseGoRunner
}

func (c *buildChecker) Name() string { return "Clean Room Build Simulation" }
func (c *buildChecker) Run(ctx context.Context) checkResult {
	tmpDir, err := os.MkdirTemp("", "release-build-*")
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Failed to create temp dir: %v", err)}
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	out, err := c.runner.BuildCode(ctx, filepath.Join(tmpDir, "tell-me-go"), "./cmd/tell-me-go")
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Clean build failed: %v\nOutput: %s", err, string(out))}
	}
	return checkResult{OK: true, Message: "Application compiles successfully from source."}
}

// testRunner implementation
type testRunner struct {
	runner releaseGoRunner
}

func (c *testRunner) Name() string { return "Test Suite Verification" }
func (c *testRunner) Run(ctx context.Context) checkResult {
	out, err := c.runner.RunTests(ctx, "./...")
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Unit/Integration tests failed: %v\nOutput: %s", err, string(out))}
	}
	return checkResult{OK: true, Message: "All tests passed (including race detector)."}
}

// linterChecker implementation
type linterChecker struct {
	runner releaseGoRunner
}

func (c *linterChecker) Name() string { return "Linter Verification" }

func (c *linterChecker) Run(ctx context.Context) checkResult {
	out, tool, err := c.runner.RunLinter(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "no supported linter found") {
			return checkResult{OK: false, Message: "No linter found (golangci-lint or staticcheck)."}
		}
		// If it's an exit status 1, it usually means issues found, but we should check output.
		if strings.Contains(err.Error(), "exit status 1") {
			return c.handleLinterResult([]byte(out), err, tool)
		}
		return checkResult{OK: false, Message: fmt.Sprintf("%s failed: %v\nOutput: %s", tool, err, out)}
	}

	return c.handleLinterResult([]byte(out), nil, tool)
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

	return checkResult{OK: false, Message: fmt.Sprintf("%s failed: %v\nOutput: %s", name, err, string(out))}
}

// architectureChecker implementation
type architectureChecker struct {
	verifier tools.ToolFunc
}

func (c *architectureChecker) Name() string { return "Architectural Integrity Verification" }

func (c *architectureChecker) Run(ctx context.Context) checkResult {
	if c.verifier == nil {
		return checkResult{OK: false, Message: "Architecture verifier is not available."}
	}
	res, err := c.verifier(ctx, nil, nil)
	if err != nil {
		return checkResult{OK: false, Message: fmt.Sprintf("Architecture check failed: %v", err)}
	}
	if strings.Contains(res.Text, "❌ FAILED") {
		return checkResult{OK: false, Message: "Layer violations or circular dependencies detected."}
	}
	return checkResult{OK: true, Message: "No architectural violations detected."}
}

func (m *releaseManager) getParallelism() int64 {
	const defaultParallelism = 2
	val := os.Getenv("TELL_ME_GO_RELEASE_PARALLELISM")
	if val == "" {
		return defaultParallelism
	}
	var p int
	if _, err := fmt.Sscanf(val, "%d", &p); err != nil {
		return defaultParallelism
	}
	if p < 1 {
		return 1
	}
	return int64(p)
}
