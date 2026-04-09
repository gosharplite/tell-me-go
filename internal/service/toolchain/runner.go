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

// goRunner executes Go toolchain commands.
type goRunner struct {
	exec           tools.CommandExecutor
	defaultTimeout time.Duration
	raceEnabled    bool
	createTemp     func(string, string) (*os.File, error)
	removeFile     func(string) error
}

// runnerOption defines a functional option for goRunner.
type runnerOption func(*goRunner)

// withFilesystem allows injecting custom temporary file management, useful for
// isolation tests or memory-only environments.
func withFilesystem(create func(string, string) (*os.File, error), remove func(string) error) runnerOption {
	return func(r *goRunner) {
		r.createTemp = create
		r.removeFile = remove
	}
}

// withDefaultTimeout sets the default timeout for goRunner commands if no deadline is set in the context.
func withDefaultTimeout(d time.Duration) runnerOption {
	return func(r *goRunner) {
		r.defaultTimeout = d
	}
}

// withRace enables or disables the race detector for tests.
func withRace(enabled bool) runnerOption {
	return func(r *goRunner) {
		r.raceEnabled = enabled
	}
}

// NewGoRunner creates a new instance of goRunner with the provided options.
func NewGoRunner(exec tools.CommandExecutor, opts ...runnerOption) *goRunner {
	r := &goRunner{
		exec:           exec,
		defaultTimeout: 5 * time.Minute,
		raceEnabled:    true,
		createTemp:     os.CreateTemp,
		removeFile:     os.Remove,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// applyTimeout returns a child context with the default timeout if no deadline is already set.
func (r *goRunner) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, r.defaultTimeout)
}

// RunTestsWithCoverage executes tests with coverage and returns a parsed report.
func (r *goRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (CoverageReport, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	var report CoverageReport

	tempName := profilePath
	if tempName == "" {
		f, err := r.createTemp("", "coverage-*.out")
		if err != nil {
			return report, fmt.Errorf("failed to create temp coverage file: %w", err)
		}
		tempName = f.Name()
		// Safe to ignore: immediate closure to prepare file for OS write by child process.
		_ = f.Close()
		// Safe to ignore: best-effort temporary file cleanup.
		defer func() { _ = r.removeFile(tempName) }()
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
func (r *goRunner) RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error) {
	ctx, cancel := r.applyTimeout(ctx)
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
func (r *goRunner) RunLinter(ctx context.Context) (output string, toolUsed string, err error) {
	ctx, cancel := r.applyTimeout(ctx)
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
func (r *goRunner) RunTests(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	args := []string{"test"}
	if r.raceEnabled {
		args = append(args, "-race")
	}
	args = append(args, path)

	return r.combinedOutput(ctx, "go", args...)
}

// BuildCode builds the code at the given path and writes it to the output binary.
func (r *goRunner) BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	return r.combinedOutput(ctx, "go", "build", "-o", outputBinary, path)
}

// CheckGovulncheck checks if govulncheck is installed.
func (r *goRunner) CheckGovulncheck(ctx context.Context) error {
	_, err := r.lookPath("govulncheck")
	if err != nil {
		return fmt.Errorf("'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest")
	}
	return nil
}

// RunModTidy runs 'go mod tidy'.
func (r *goRunner) RunModTidy(ctx context.Context) ([]byte, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	return r.combinedOutput(ctx, "go", "mod", "tidy")
}

// FormatCode runs 'go fmt' on the specified path.
func (r *goRunner) FormatCode(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	return r.combinedOutput(ctx, "go", "fmt", path)
}

// GetPackageList runs 'go list -json' on the specified path.
func (r *goRunner) GetPackageList(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	return r.combinedOutput(ctx, "go", "list", "-json", path)
}

// GetGoDoc runs 'go doc' for the specified symbol.
func (r *goRunner) GetGoDoc(ctx context.Context, symbol string) ([]byte, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	return r.combinedOutput(ctx, "go", "doc", symbol)
}

// GetModulePath returns the Go module path.
func (r *goRunner) GetModulePath(ctx context.Context) (string, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	out, err := r.output(ctx, "go", "list", "-m")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetModuleDir returns the Go module directory.
func (r *goRunner) GetModuleDir(ctx context.Context) (string, error) {
	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	out, err := r.output(ctx, "go", "list", "-m", "-f", "{{.Dir}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// combinedOutput executes a command and returns its combined standard output and standard error.
func (r *goRunner) combinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.exec.CombinedOutput(ctx, name, args...)
}

// output executes a command and returns its standard output.
func (r *goRunner) output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return r.exec.Output(ctx, name, args...)
}

// lookPath proxies the LookPath call to the underlying executor.
func (r *goRunner) lookPath(file string) (string, error) {
	return r.exec.LookPath(file)
}
