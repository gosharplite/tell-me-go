package toolchain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const defaultToolTimeout = 30 * time.Second

// ErrNoSupportedLinter is returned when neither golangci-lint nor staticcheck is found.
var ErrNoSupportedLinter = errors.New("no supported linter found (golangci-lint or staticcheck)")

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
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	var report CoverageReport

	tempName := profilePath
	if tempName == "" {
		f, err := os.CreateTemp("", "coverage-*.out")
		if err != nil {
			return report, fmt.Errorf("failed to create temp coverage file: %w", err)
		}
		tempName = f.Name()
		// Safe to ignore: immediate closure to prepare file for OS write by child process.
		_ = f.Close()
		// Safe to ignore: best-effort temporary file cleanup.
		defer func() { _ = os.Remove(tempName) }()
	}

	args := []string{"test"}
	if short {
		args = append(args, "-short")
	}
	args = append(args, "-coverprofile="+tempName, path)

	out, testErr := r.combinedOutput(ctx, "go", args...)
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
	sumOut, summaryErr := r.combinedOutput(ctx, "go", "tool", "cover", "-func="+tempName)
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
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	args := []string{"test", "-bench=" + benchRegex, "-benchmem", "-run=^$", path}
	out, err := r.combinedOutput(ctx, "go", args...)
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
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	if _, err := r.lookPath("golangci-lint"); err == nil {
		out, err := r.combinedOutput(ctx, "golangci-lint", "run")
		return string(out), "golangci-lint", err
	}
	if _, err := r.lookPath("staticcheck"); err == nil {
		out, err := r.combinedOutput(ctx, "staticcheck", "./...")
		return string(out), "staticcheck", err
	}
	return "", "", ErrNoSupportedLinter
}

// RunTests runs project tests using the standard Go test tool.
func (r *GoRunner) RunTests(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	return r.combinedOutput(ctx, "go", "test", "-race", path)
}

// BuildCode builds the code at the given path and writes it to the output binary.
func (r *GoRunner) BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	return r.combinedOutput(ctx, "go", "build", "-o", outputBinary, path)
}

// CheckGovulncheck checks if govulncheck is installed.
func (r *GoRunner) CheckGovulncheck(ctx context.Context) error {
	_, err := r.lookPath("govulncheck")
	if err != nil {
		return fmt.Errorf("'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest")
	}
	return nil
}

// RunModTidy runs 'go mod tidy'.
func (r *GoRunner) RunModTidy(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	return r.combinedOutput(ctx, "go", "mod", "tidy")
}

// FormatCode runs 'go fmt' on the specified path.
func (r *GoRunner) FormatCode(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	return r.combinedOutput(ctx, "go", "fmt", path)
}

// GetPackageList runs 'go list -json' on the specified path.
func (r *GoRunner) GetPackageList(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	return r.combinedOutput(ctx, "go", "list", "-json", path)
}

// GetGoDoc runs 'go doc' for the specified symbol.
func (r *GoRunner) GetGoDoc(ctx context.Context, symbol string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	return r.combinedOutput(ctx, "go", "doc", symbol)
}

// GetModulePath returns the Go module path.
func (r *GoRunner) GetModulePath(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	out, err := r.output(ctx, "go", "list", "-m")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetModuleDir returns the Go module directory.
func (r *GoRunner) GetModuleDir(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultToolTimeout)
	defer cancel()

	out, err := r.output(ctx, "go", "list", "-m", "-f", "{{.Dir}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// combinedOutput executes a command and returns its combined standard output and standard error.
func (r *GoRunner) combinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.exec.CombinedOutput(ctx, name, args...)
}

// output executes a command and returns its standard output.
func (r *GoRunner) output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.exec.Output(ctx, name, args...)
}

// lookPath proxies the LookPath call to the underlying executor.
func (r *GoRunner) lookPath(file string) (string, error) {
	return r.exec.LookPath(file)
}
