package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// CoverageReport encapsulates the result of running tests with coverage.
type CoverageReport struct {
	TestOutput    string
	SummaryOutput string
	PassedCount   int
	CoveragePct   string // e.g. "85.0%"
	NoGoFiles     bool
}

// GoRunner executes Go toolchain commands.
type GoRunner struct {
	exec tools.CommandExecutor
}

// NewGoRunner creates a new instance of GoRunner.
func NewGoRunner(exec tools.CommandExecutor) *GoRunner {
	return &GoRunner{exec: exec}
}

// RunTestsWithCoverage executes tests with coverage and returns a parsed report.
func (r *GoRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (CoverageReport, error) {
	var report CoverageReport

	tempName := profilePath
	if tempName == "" {
		f, err := os.CreateTemp("", "coverage-*.out")
		if err != nil {
			return report, fmt.Errorf("failed to create temp coverage file: %w", err)
		}
		tempName = f.Name()
		_ = f.Close()
		defer func() { _ = os.Remove(tempName) }()
	}

	args := []string{"test"}
	if short {
		args = append(args, "-short")
	}
	args = append(args, "-coverprofile="+tempName, path)

	out, testErr := r.exec.CombinedOutput(ctx, "go", args...)
	outStr := string(out)
	report.TestOutput = outStr

	if testErr != nil {
		if strings.Contains(outStr, "no Go files") || strings.Contains(outStr, "[no test files]") || strings.Contains(outStr, "no packages to test") {
			report.NoGoFiles = true
			report.CoveragePct = "0.0%"
			return report, nil
		}
		return report, fmt.Errorf("test execution failed: %w (output: %s)", testErr, outStr)
	}

	// Count packages
	lines := strings.Split(strings.TrimSpace(outStr), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ok") {
			report.PassedCount++
		}
	}

	// Get summary
	sumOut, summaryErr := r.exec.CombinedOutput(ctx, "go", "tool", "cover", "-func="+tempName)
	if summaryErr != nil {
		return report, fmt.Errorf("coverage summary execution failed: %w", summaryErr)
	}

	report.SummaryOutput = string(sumOut)
	re := regexp.MustCompile(`total:\s+\(statements\)\s+(\d+\.\d+)%`)
	matches := re.FindStringSubmatch(report.SummaryOutput)
	if len(matches) > 1 {
		report.CoveragePct = matches[1] + "%"
	} else {
		report.CoveragePct = "N/A"
	}

	return report, nil
}

// RunBenchmarks runs standard Go benchmarks in the target path.
func (r *GoRunner) RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error) {
	args := []string{"test", "-bench=" + benchRegex, "-benchmem", "-run=^$", path}
	out, err := r.exec.CombinedOutput(ctx, "go", args...)
	outStr := string(out)

	if err != nil {
		if strings.Contains(outStr, "no Go files") || strings.Contains(outStr, "[no test files]") || strings.Contains(outStr, "no packages to test") {
			return "No Go files found in target path to benchmark", nil
		}
		return "", fmt.Errorf("benchmark failed: %w (output: %s)", err, outStr)
	}
	return outStr, nil
}

// RunLinter discovers and runs the first available linter (golangci-lint or staticcheck).
func (r *GoRunner) RunLinter(ctx context.Context) (output string, toolUsed string, err error) {
	if _, err := r.exec.LookPath("golangci-lint"); err == nil {
		out, err := r.exec.CombinedOutput(ctx, "golangci-lint", "run")
		return string(out), "golangci-lint", err
	}
	if _, err := r.exec.LookPath("staticcheck"); err == nil {
		out, err := r.exec.CombinedOutput(ctx, "staticcheck", "./...")
		return string(out), "staticcheck", err
	}
	return "", "", errors.New("no supported linter found (golangci-lint or staticcheck)")
}

// CombinedOutput executes a command and returns its combined standard output and standard error.
func (r *GoRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.exec.CombinedOutput(ctx, name, args...)
}

// LookPath proxies the LookPath call to the underlying executor.
func (r *GoRunner) LookPath(file string) (string, error) {
	return r.exec.LookPath(file)
}
