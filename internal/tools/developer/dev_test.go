// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertions to ensure the infrastructure strictly implements the domain interfaces.
// This creates a hard AST reference to clear dead-code false positives in static analysis tools
// that do not respect //nolint directives.
var _ domain_security.ActionConfirmer = (*toolstest.MockSecurityManager)(nil)
var _ domain_security.TerminalController = (*toolstest.MockSecurityManager)(nil)
var _ domain_security.UserInteractor = (*toolstest.MockInteractor)(nil)

type mockDevExecutor struct {
	executeFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockDevExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, name, args...)
	}
	return []byte("success"), nil
}

type mockValidator struct {
	domain_security.CommandValidator
	isSafeFunc func(command string) (bool, string)
}

func (mv *mockValidator) IsSafe(command string) (bool, string) {
	if mv.isSafeFunc != nil {
		return mv.isSafeFunc(command)
	}
	return mv.CommandValidator.IsSafe(command)
}

type mockGoRunner struct {
	runTestsWithCoverageFunc func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	runBenchmarksFunc        func(ctx context.Context, path string, benchRegex string) (string, error)
	runLinterFunc            func(ctx context.Context) (string, string, error)
	checkGovulncheckFunc     func(ctx context.Context) error
	runModTidyFunc           func(ctx context.Context) ([]byte, error)
	formatCodeFunc           func(ctx context.Context, path string) ([]byte, error)
	runTestsFunc             func(ctx context.Context, path string) ([]byte, error)
}

func (m *mockGoRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
	if m.runTestsWithCoverageFunc != nil {
		return m.runTestsWithCoverageFunc(ctx, path, short, profilePath)
	}
	return toolchain.CoverageReport{}, nil
}

func (m *mockGoRunner) RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error) {
	if m.runBenchmarksFunc != nil {
		return m.runBenchmarksFunc(ctx, path, benchRegex)
	}
	return "", nil
}

func (m *mockGoRunner) RunLinter(ctx context.Context) (string, string, error) {
	if m.runLinterFunc != nil {
		return m.runLinterFunc(ctx)
	}
	return "", "golangci-lint", nil
}

func (m *mockGoRunner) CheckGovulncheck(ctx context.Context) error {
	if m.checkGovulncheckFunc != nil {
		return m.checkGovulncheckFunc(ctx)
	}
	return nil
}

func (m *mockGoRunner) RunModTidy(ctx context.Context) ([]byte, error) {
	if m.runModTidyFunc != nil {
		return m.runModTidyFunc(ctx)
	}
	return []byte("success"), nil
}

func (m *mockGoRunner) FormatCode(ctx context.Context, path string) ([]byte, error) {
	if m.formatCodeFunc != nil {
		return m.formatCodeFunc(ctx, path)
	}
	return []byte("success"), nil
}

func (m *mockGoRunner) RunTests(ctx context.Context, path string) ([]byte, error) {
	if m.runTestsFunc != nil {
		return m.runTestsFunc(ctx, path)
	}
	return []byte("success"), nil
}

func setupDevManager(t *testing.T) (*devManager, *mockDevExecutor, *toolstest.MockSecurityManager) {
	t.Helper()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	executor := &mockDevExecutor{}
	runner := &mockGoRunner{}
	m := &devManager{
		sm:             sm,
		validator:      &toolstest.MockCommandValidator{},
		executor:       executor,
		runner:         runner,
		createTempFile: os.CreateTemp,
	}
	return m, executor, sm
}

func TestCheckVulnerabilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		lookPathErr  error
		executeOut   string
		executeErr   error
		decline      bool
		wantSubstr   string
		wantErr      bool
		wantAuthFail bool
	}{
		{
			name:       "Success - no vulnerabilities",
			executeOut: "No vulnerabilities found.",
			wantSubstr: "No vulnerabilities",
		},
		{
			name:        "Tool missing",
			lookPathErr: fmt.Errorf("'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest"),
			wantSubstr:  "'govulncheck' is not installed",
			wantErr:     true,
		},
		{
			name:       "Vulnerabilities found",
			executeOut: "VULNERABILITY FOUND",
			executeErr: errors.New("exit status 3"),
			wantSubstr: "VULNERABILITY FOUND",
			wantErr:    false, // We no longer return an error, it's inside the text!
		},
		{
			name:       "Execution failure no output",
			executeErr: errors.New("something went wrong"),
			wantSubstr: "Govulncheck execution failed",
			wantErr:    true,
		},
		{
			name:       "user declined",
			decline:    true,
			wantSubstr: "Action denied by user.",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, sm := setupDevManager(t)
			if tt.decline {
				sm.AllowAll = false
				m.validator = &mockValidator{
					CommandValidator: m.validator,
					isSafeFunc: func(command string) (bool, string) {
						if strings.Contains(command, "vulncheck") {
							return false, "forced prompt for test"
						}
						return true, ""
					},
				}
			} else {
				m.runner.(*mockGoRunner).checkGovulncheckFunc = func(ctx context.Context) error {
					return tt.lookPathErr
				}
				executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
					return []byte(tt.executeOut), tt.executeErr
				}
			}

			res, err := m.checkVulnerabilities(context.Background(), nil, nil)
			actualErr := err
			if actualErr == nil {
				actualErr = res.Error
			}

			if (actualErr != nil) != tt.wantErr {
				t.Errorf("checkVulnerabilities() error = %v, wantErr %v", actualErr, tt.wantErr)
			}
			fullOutput := res.Text
			if actualErr != nil {
				fullOutput += " " + actualErr.Error()
			}
			if !strings.Contains(fullOutput, tt.wantSubstr) {
				t.Errorf("expected substring %q, got res.Text=%q, err=%v", tt.wantSubstr, res.Text, actualErr)
			}
		})
	}
}

func setupCoverageMock(t *testing.T, m *devManager, executeErr error, summaryOut string, tempErr error, noGoFiles bool) {
	runner := m.runner.(*mockGoRunner)
	runner.runTestsWithCoverageFunc = func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
		if tempErr != nil {
			return toolchain.CoverageReport{}, tempErr
		}
		if executeErr != nil {
			return toolchain.CoverageReport{}, executeErr
		}
		if noGoFiles {
			return toolchain.CoverageReport{NoGoFiles: true}, nil
		}
		return toolchain.CoverageReport{SummaryOutput: summaryOut}, nil
	}
}

func TestGetCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		executeErr error
		summaryOut string
		tempErr    error
		noGoFiles  bool
		decline    bool
		wantSubstr string
		wantErr    bool
	}{
		{
			name:       "Success with 100% coverage",
			summaryOut: "total:			(statements)		100.0%",
			wantSubstr: "100.0%",
		},
		{
			name:       "Success with partial coverage",
			summaryOut: "github.com/gosharplite/tell-me-go/internal/tools/go:35:	Register		100.0%\ntotal:			(statements)		85.0%",
			wantSubstr: "85.0%",
		},
		{
			name:       "Failure due to test errors",
			executeErr: errors.New("exit status 1"),
			wantSubstr: "coverage test failed",
			wantErr:    true,
		},
		{
			name:       "Failure due to no Go files",
			noGoFiles:  true,
			wantSubstr: "0.0% coverage (No Go files found in target path to test)",
			wantErr:    false,
		},
		{
			name:       "user declined",
			decline:    true,
			wantSubstr: "declined",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, sm := setupDevManager(t)
			if tt.decline {
				sm.AllowAll = false
				m.validator = &mockValidator{
					CommandValidator: m.validator,
					isSafeFunc: func(command string) (bool, string) {
						if strings.Contains(command, "coverage") {
							return false, "forced prompt for test"
						}
						return true, ""
					},
				}
			} else {
				setupCoverageMock(t, m, tt.executeErr, tt.summaryOut, tt.tempErr, tt.noGoFiles)
			}

			res, err := m.getCoverage(context.Background(), nil, nil)
			actualErr := err
			if actualErr == nil {
				actualErr = res.Error
			}

			if tt.wantErr {
				require.Error(t, actualErr)
				assert.Contains(t, actualErr.Error(), tt.wantSubstr)
			} else {
				require.NoError(t, actualErr)
				assert.Contains(t, res.Text, tt.wantSubstr)
			}
		})
	}
}

func TestGoTidy(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

	res, err := m.goTidy(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("goTidy() unexpected error = %v", err)
	}
	if !strings.Contains(res.Text, "Success") {
		t.Errorf("expected Success in result, got %q", res.Text)
	}
}

func TestRunBenchmark(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	m.runner.(*mockGoRunner).runBenchmarksFunc = func(ctx context.Context, path string, benchRegex string) (string, error) {
		return "BenchmarkResult", nil
	}

	res, err := m.runBenchmark(context.Background(), map[string]interface{}{"path": "./...", "bench": "BenchmarkFoo"}, nil)
	if err != nil {
		t.Errorf("runBenchmark() unexpected error = %v", err)
	}
	if !strings.Contains(res.Text, "BenchmarkResult") {
		t.Errorf("expected BenchmarkResult in result, got %q", res.Text)
	}
}

func TestRunLinter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		lookPath   string
		executeOut string
		executeErr error
		wantSubstr string
		wantErr    bool
		decline    bool
	}{
		{
			name:       "golangci-lint success",
			lookPath:   "golangci-lint",
			wantSubstr: "Linter passed successfully",
		},
		{
			name:       "staticcheck success",
			lookPath:   "staticcheck",
			wantSubstr: "Linter passed successfully",
		},
		{
			name:       "linter issues found",
			lookPath:   "staticcheck",
			executeOut: "problem at line 1",
			executeErr: errors.New("exit status 1"),
			wantSubstr: "Linter (staticcheck) failed or found issues:",
			wantErr:    false,
		},
		{
			name:       "no linter found",
			lookPath:   "none",
			wantErr:    true,
			wantSubstr: "no supported linter found",
		},
		{
			name:       "user declined",
			lookPath:   "golangci-lint",
			decline:    true,
			wantSubstr: "Action denied by user.",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, sm := setupDevManager(t)
			if tt.decline {
				sm.AllowAll = false
				m.validator = &mockValidator{
					CommandValidator: m.validator,
					isSafeFunc: func(command string) (bool, string) {
						if strings.Contains(command, "lint") {
							return false, "forced prompt for test"
						}
						return true, ""
					},
				}
			}

			// Inject mock behavior into runner
			m.runner.(*mockGoRunner).runLinterFunc = func(ctx context.Context) (string, string, error) {
				if tt.lookPath == "none" {
					return "", "", errors.New("no supported linter found")
				}
				return tt.executeOut, tt.lookPath, tt.executeErr
			}

			res, err := m.runLinter(context.Background(), nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("runLinter() error = %v, wantErr %v", err, tt.wantErr)
			}
			fullOutput := res.Text
			if err != nil {
				fullOutput += " " + err.Error()
			}
			if !strings.Contains(fullOutput, tt.wantSubstr) {
				t.Errorf("expected %q, got res.Text=%q, err=%v", tt.wantSubstr, res.Text, err)
			}
		})
	}
}

func TestGoTidy_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		executeErr error
		cmdFail    string
		decline    bool
	}{
		{
			name:       "go mod tidy fails",
			executeErr: errors.New("tidy error"),
			cmdFail:    "tidy",
		},
		{
			name:       "go fmt fails",
			executeErr: errors.New("fmt error"),
			cmdFail:    "fmt",
		},
		{
			name:    "user declined",
			decline: true,
		},
		{
			name:       "error with empty output",
			executeErr: errors.New("tidy error"),
			cmdFail:    "tidy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, sm := setupDevManager(t)
			if tt.decline {
				sm.AllowAll = false
				m.validator = &mockValidator{
					CommandValidator: m.validator,
					isSafeFunc: func(command string) (bool, string) {
						if strings.Contains(command, "tidy") {
							return false, "forced prompt for test"
						}
						return true, ""
					},
				}
			} else {
				runner := m.runner.(*mockGoRunner)
				if tt.cmdFail == "tidy" {
					runner.runModTidyFunc = func(ctx context.Context) ([]byte, error) {
						// Return empty output when testing the error+empty path
						if tt.name == "error with empty output" {
							return nil, tt.executeErr
						}
						return []byte("failed"), tt.executeErr
					}
				} else {
					runner.formatCodeFunc = func(ctx context.Context, path string) ([]byte, error) {
						return []byte("failed"), tt.executeErr
					}
				}
			}

			res, err := m.goTidy(context.Background(), nil, nil)
			if tt.name == "error with empty output" {
				if err == nil {
					t.Error("expected error for empty output, got nil")
				}
				return
			}
			if tt.decline {
				if err == nil {
					t.Error("expected error for user declined, got nil")
				}
				if !strings.Contains(res.Text, "Action denied by user.") {
					t.Errorf("expected 'Action denied by user.', got %q", res.Text)
				}
				return
			}
			if err != nil {
				t.Errorf("expected nil error for %s, got %v", tt.name, err)
			}
			displayName := "Go mod tidy/fmt"
			if !strings.Contains(res.Text, displayName+" failed or found issues:") {
				t.Errorf("expected failure text in result, got %q", res.Text)
			}
		})
	}
}

func TestRunBenchmark_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		executeOut string
		executeErr error
		decline    bool
		wantSubstr string
		wantErr    bool
	}{
		{
			name:       "General failure",
			executeOut: "error output",
			executeErr: errors.New("benchmark failed"),
			wantSubstr: "benchmark failed or found issues:",
			wantErr:    true,
		},
		{
			name:       "No Go files",
			executeOut: "No Go files found in target path to benchmark",
			executeErr: nil,
			wantSubstr: "No Go files found in target path to benchmark",
			wantErr:    false,
		},
		{
			name:       "user declined",
			decline:    true,
			wantSubstr: "declined",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, sm := setupDevManager(t)
			if tt.decline {
				sm.AllowAll = false
				m.validator = &mockValidator{
					CommandValidator: m.validator,
					isSafeFunc: func(command string) (bool, string) {
						if strings.Contains(command, "benchmark") {
							return false, "forced prompt for test"
						}
						return true, ""
					},
				}
			} else {
				m.runner.(*mockGoRunner).runBenchmarksFunc = func(ctx context.Context, path string, benchRegex string) (string, error) {
					return tt.executeOut, tt.executeErr
				}
			}

			res, err := m.runBenchmark(context.Background(), nil, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("expected wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), tt.wantSubstr) {
					t.Errorf("expected %q in error, got %q", tt.wantSubstr, err.Error())
				}
			} else {
				if !strings.Contains(res.Text, tt.wantSubstr) {
					t.Errorf("expected %q in text, got %q", tt.wantSubstr, res.Text)
				}
			}
		})
	}
}

func TestRunTests(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("PASS"), nil
	}

	res, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."}, nil)
	if err != nil {
		t.Errorf("runTests() unexpected error = %v", err)
	}
	if !strings.Contains(res.Text, "PASS") {
		t.Errorf("expected PASS in result, got %q", res.Text)
	}
}

func TestRunTests_Violations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "Unauthorized tool",
			command: "rm -rf /",
		},
		{
			name:    "Path safety violation",
			command: "go test ../../../etc/passwd",
		},
		{
			name:    "Invalid command structure",
			command: "go test ; rm -rf /",
		},
		{
			name:    "Allowed script run_tests.sh",
			command: "./run_tests.sh",
		},
		{
			name:    "Allowed tool make",
			command: "make test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, _ := setupDevManager(t)
			if tt.name == "Path safety violation" {
				m.validator.(*toolstest.MockCommandValidator).CheckPathSafetyFunc = func(parts []string) (bool, string) {
					return false, "forced violation"
				}
			}
			if tt.name == "Invalid command structure" {
				m.validator.(*toolstest.MockCommandValidator).ValidateStructureFunc = func(parts []string) error {
					return errors.New("forced structure error")
				}
			}
			// For allowed commands, set up a successful executor
			if tt.name == "Allowed script run_tests.sh" || tt.name == "Allowed tool make" {
				executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
					return []byte("PASS"), nil
				}
				_, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command}, nil)
				if err != nil {
					t.Errorf("expected no error for %s, got %v", tt.name, err)
				}
				return
			}
			_, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command}, nil)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestRunTests_Failure(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("FAIL"), errors.New("exit status 1")
	}

	res, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."}, nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !strings.Contains(res.Text, "Test execution failed or found issues:") {
		t.Errorf("expected Test execution failed or found issues: in result, got %q", res.Text)
	}
}

func TestRunTests_Decline(t *testing.T) {
	t.Parallel()
	m, _, sm := setupDevManager(t)
	sm.AllowAll = false
	m.validator = &mockValidator{
		CommandValidator: m.validator,
		isSafeFunc: func(command string) (bool, string) {
			if strings.Contains(command, "test") {
				return false, "forced prompt for test"
			}
			return true, ""
		},
	}

	res, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."}, nil)
	require.Error(t, err)
	assert.Contains(t, res.Text, "Action denied by user.")
}

func TestNewDevManager(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}
	runner := &mockGoRunner{}
	m := newDevManager(sm, validator, runner)
	assert.NotNil(t, m)
	assert.NotNil(t, m.executor)
}

func TestAuthorizeAction_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		authorizeFunc func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
		wantApproved  bool
		wantErr       bool
		errContains   string
	}{
		{
			name: "Auth backend error",
			authorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
				return false, fmt.Errorf("timeout")
			},
			wantApproved: false,
			wantErr:      true,
			errContains:  "authorization error: timeout",
		},
		{
			name: "User declines",
			authorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
				return false, nil
			},
			wantApproved: false,
			wantErr:      false,
		},
		{
			name: "User approves",
			authorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
				return true, nil
			},
			wantApproved: true,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, sm := setupDevManager(t)
			sm.AllowAll = false
			sm.AuthorizeFunc = tt.authorizeFunc

			approved, err := m.authorizeAction(context.Background(), "test_action", "echo hello", "Run a test command")

			if approved != tt.wantApproved {
				t.Errorf("approved = %v, want %v", approved, tt.wantApproved)
			}
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want contains %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestFormatExecutionResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		toolName        string
		out             []byte
		execErr         error
		truncate        int
		emptySuccessMsg string
		wantSubstr      string
		wantErrObj      bool
	}{
		{
			name:       "Execution failure no output",
			toolName:   "Linter",
			out:        []byte(""),
			execErr:    errors.New("crash"),
			wantErrObj: true,
			wantSubstr: "Linter execution failed",
		},
		{
			name:       "Execution failure with output",
			toolName:   "Linter",
			out:        []byte("problem found"),
			execErr:    errors.New("exit status 1"),
			wantSubstr: "Linter failed or found issues",
		},
		{
			name:            "Success with no output",
			toolName:        "Linter",
			out:             []byte(""),
			execErr:         nil,
			emptySuccessMsg: "Linter passed successfully.",
			wantSubstr:      "Linter passed successfully",
		},
		{
			name:       "Success with output",
			toolName:   "Linter",
			out:        []byte("some warnings"),
			execErr:    nil,
			wantSubstr: "some warnings",
		},
		{
			name:       "Output truncation",
			toolName:   "linter",
			out:        []byte(strings.Repeat("a", 200)),
			execErr:    nil,
			truncate:   100,
			wantSubstr: strings.Repeat("a", 100),
		},
		{
			name:       "Empty tool name handling",
			toolName:   "",
			out:        []byte("output"),
			execErr:    errors.New("error"),
			wantSubstr: " failed or found issues:\noutput",
		},
		{
			name:       "Zero truncate limit does not truncate",
			toolName:   "test",
			out:        []byte(strings.Repeat("a", 200)),
			execErr:    nil,
			truncate:   0,
			wantSubstr: strings.Repeat("a", 200),
		},
		{
			name:       "Error with single char output",
			toolName:   "TestRunner",
			out:        []byte("X"),
			execErr:    errors.New("fail"),
			wantSubstr: "TestRunner failed or found issues:\nX",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := formatExecutionResult(tt.toolName, tt.out, tt.execErr, tt.truncate, tt.emptySuccessMsg)
			if tt.wantErrObj {
				assert.Error(t, res.Error)
				assert.Contains(t, res.Error.Error(), tt.wantSubstr)
			} else {
				assert.NoError(t, res.Error)
				assert.Contains(t, res.Text, tt.wantSubstr)
			}
		})
	}
}

func TestExecuteWithHeartbeat_Telemetry(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)
	m.heartbeatInterval = 1 * time.Millisecond

	hb := make(chan struct{}, 100)
	wait := make(chan struct{})
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		<-wait
		return []byte("done"), nil
	}

	// Trigger execution in a goroutine
	done := make(chan struct{})
	go func() {
		_, _ = m.executeWithHeartbeat(context.Background(), hb, "test", "cmd", "reason", func() ([]byte, error) {
			return m.executor.Execute(context.Background(), "echo", "hi")
		})
		close(done)
	}()

	// We expect at least a couple of heartbeats
	select {
	case <-hb:
		// Observed first heartbeat
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected heartbeat, but none received")
	}

	close(wait) // Let the executor finish
	<-done
}

func TestRunWithHeartbeat_Resilience(t *testing.T) {
	t.Parallel()

	t.Run("Channel full does not block fn", func(t *testing.T) {
		t.Parallel()
		m, _, _ := setupDevManager(t)
		m.heartbeatInterval = 1 * time.Millisecond

		// Buffer of 1, pre-fill it so subsequent heartbeats hit default:
		hb := make(chan struct{}, 1)
		hb <- struct{}{}

		var fnCalled bool
		err := m.runWithHeartbeat(context.Background(), hb, "test", "cmd", "reason", func() error {
			time.Sleep(50 * time.Millisecond) // Let several ticks fire into full channel
			fnCalled = true
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, fnCalled, "fn should have been called despite full heartbeat channel")
	})

	t.Run("Fn error propagated through heartbeat", func(t *testing.T) {
		t.Parallel()
		m, _, _ := setupDevManager(t)
		m.heartbeatInterval = 1 * time.Millisecond
		hb := make(chan struct{}, 10)

		expectedErr := errors.New("fn failed")
		err := m.runWithHeartbeat(context.Background(), hb, "test", "cmd", "reason", func() error {
			return expectedErr
		})

		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("Nil hb runs fn successfully", func(t *testing.T) {
		t.Parallel()
		m, _, _ := setupDevManager(t)
		m.heartbeatInterval = 1 * time.Millisecond

		var fnCalled bool
		err := m.runWithHeartbeat(context.Background(), nil, "test", "cmd", "reason", func() error {
			fnCalled = true
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, fnCalled, "fn should have been called with nil hb")
	})
}

func TestSecurityRemediation(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

	t.Run("getCoverage flag injection", func(t *testing.T) {
		_, err := m.getCoverage(context.Background(), map[string]interface{}{"path": "-config=evil.yml"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot start with a hyphen")
	})

	t.Run("runBenchmark flag injection", func(t *testing.T) {
		_, err := m.runBenchmark(context.Background(), map[string]interface{}{"bench": "-evil"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot start with a hyphen")
	})

	t.Run("getCoverage sandbox evasion", func(t *testing.T) {
		m.validator.(*toolstest.MockCommandValidator).CheckPathSafetyFunc = func(parts []string) (bool, string) {
			return false, "security violation"
		}
		_, err := m.getCoverage(context.Background(), map[string]interface{}{"path": "../../etc/passwd"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security violation")
	})

	t.Run("runBenchmark sandbox evasion", func(t *testing.T) {
		m.validator.(*toolstest.MockCommandValidator).CheckPathSafetyFunc = func(parts []string) (bool, string) {
			return false, "security violation"
		}
		_, err := m.runBenchmark(context.Background(), map[string]interface{}{"path": "../../etc/passwd"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security violation")
	})
}

func TestDevManager_Options(t *testing.T) {
	t.Parallel()
	customInterval := 42 * time.Second
	m := newDevManager(nil, nil, nil, withHeartbeatInterval(customInterval))

	if m.heartbeatInterval != customInterval {
		t.Errorf("expected interval %v, got %v", customInterval, m.heartbeatInterval)
	}
}

func TestRunTests_Timeout(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)

	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return []byte("PASS"), nil
		}
	}

	// Test with a very short timeout that should trigger
	res, err := m.runTests(context.Background(), map[string]interface{}{
		"command": "go test ./...",
		"timeout": 1, // 1 second is plenty for the 100ms mock, but let's test the trigger
	}, nil)
	assert.NoError(t, err)
	assert.Contains(t, res.Text, "PASS")

	// Test actual trigger by making mock wait longer
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	res, err = m.runTests(context.Background(), map[string]interface{}{
		"command": "go test ./...",
		"timeout": 1,
	}, nil)

	// formatExecutionResult will catch the context error
	assert.NoError(t, err)
	assert.Contains(t, res.Text, "context deadline exceeded")
}

func TestRunTests_FormatExecutionError_NonContext(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)

	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("exec format error")
	}

	_, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."}, nil)
	if err == nil {
		t.Fatal("expected error from formatExecutionResult when output is empty and error is non-context")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Errorf("expected 'execution failed' in error, got: %v", err)
	}
}

func TestRunTests_EmptyCommand(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

	_, err := m.runTests(context.Background(), map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "command argument is required") {
		t.Errorf("expected 'command argument is required', got: %v", err)
	}
}

func TestRunTests_UnmarshalError(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

	_, err := m.runTests(context.Background(), map[string]interface{}{"command": 123}, nil)
	if err == nil {
		t.Fatal("expected error from unmarshal args")
	}
}

func TestCheckVulnerabilities_GovulncheckError(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

	m.runner.(*mockGoRunner).checkGovulncheckFunc = func(ctx context.Context) error {
		return fmt.Errorf("govulncheck internal error")
	}

	_, err := m.checkVulnerabilities(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error from CheckGovulncheck failure")
	}
	if !strings.Contains(err.Error(), "govulncheck internal error") {
		t.Errorf("expected 'govulncheck internal error' in error, got: %v", err)
	}
}

func TestGoTidy_ModTidySuccessFormatFail(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	runner := m.runner.(*mockGoRunner)

	runner.runModTidyFunc = func(ctx context.Context) ([]byte, error) {
		return []byte("tidy output"), nil
	}
	runner.formatCodeFunc = func(ctx context.Context, path string) ([]byte, error) {
		return nil, fmt.Errorf("format failure")
	}

	_, err := m.goTidy(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected Go error when FormatCode fails with nil output")
	}
	if !strings.Contains(err.Error(), "Go mod tidy/fmt execution failed") {
		t.Errorf("expected 'Go mod tidy/fmt execution failed' in error, got: %v", err)
	}
}

func TestGoTidy_ModTidyFailsEmptyOutput(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	runner := m.runner.(*mockGoRunner)

	runner.runModTidyFunc = func(ctx context.Context) ([]byte, error) {
		return nil, fmt.Errorf("mod tidy crash")
	}

	_, err := m.goTidy(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected Go error when RunModTidy fails with nil output")
	}
	if !strings.Contains(err.Error(), "Go mod tidy/fmt execution failed") {
		t.Errorf("expected 'Go mod tidy/fmt execution failed' in error, got: %v", err)
	}
}

func TestRunTests_ValidateStructureError(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	m.validator.(*toolstest.MockCommandValidator).ValidateStructureFunc = func(parts []string) error {
		return fmt.Errorf("forbidden shell operator")
	}

	_, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."}, nil)
	if err == nil {
		t.Fatal("expected error from ValidateStructure failure")
	}
	if !strings.Contains(err.Error(), "forbidden shell operator") {
		t.Errorf("expected 'forbidden shell operator' in error, got: %v", err)
	}
}

func TestGetCoverage_UnmarshalError(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	_, err := m.getCoverage(context.Background(), map[string]interface{}{"path": 123}, nil)
	if err == nil {
		t.Fatal("expected error from unmarshal args")
	}
}

func TestRunBenchmark_UnmarshalError(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	_, err := m.runBenchmark(context.Background(), map[string]interface{}{"bench": 123}, nil)
	if err == nil {
		t.Fatal("expected error from unmarshal args")
	}
}

func TestRunBenchmark_FormatExecutionResult_NoError(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)
	runner := m.runner.(*mockGoRunner)
	runner.runBenchmarksFunc = func(ctx context.Context, path string, benchRegex string) (string, error) {
		return "bench results", nil
	}

	res, err := m.runBenchmark(context.Background(), map[string]interface{}{
		"path":  "./...",
		"bench": "BenchmarkFoo",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != nil {
		t.Errorf("unexpected res.Error: %v", res.Error)
	}
	if !strings.Contains(res.Text, "bench results") {
		t.Errorf("expected 'bench results' in Text, got: %q", res.Text)
	}
}

func TestGetCoverage_AuthorizationError(t *testing.T) {
	t.Parallel()
	m, _, sm := setupDevManager(t)
	sm.AllowAll = false
	sm.AuthorizeFunc = func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
		return false, fmt.Errorf("auth backend timeout")
	}

	_, err := m.getCoverage(context.Background(), map[string]interface{}{"path": "./..."}, nil)
	if err == nil {
		t.Fatal("expected error from authorization failure")
	}
	if !strings.Contains(err.Error(), "auth backend timeout") {
		t.Errorf("expected 'auth backend timeout' in error, got: %v", err)
	}
}

func TestRunBenchmark_AuthorizationError(t *testing.T) {
	t.Parallel()
	m, _, sm := setupDevManager(t)
	sm.AllowAll = false
	sm.AuthorizeFunc = func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
		return false, fmt.Errorf("auth backend timeout")
	}

	_, err := m.runBenchmark(context.Background(), map[string]interface{}{
		"path":  "./...",
		"bench": ".",
	}, nil)
	if err == nil {
		t.Fatal("expected error from authorization failure")
	}
	if !strings.Contains(err.Error(), "auth backend timeout") {
		t.Errorf("expected 'auth backend timeout' in error, got: %v", err)
	}
}

func TestRunLinter_AuthorizationError(t *testing.T) {
	t.Parallel()
	m, _, sm := setupDevManager(t)
	sm.AllowAll = false
	sm.AuthorizeFunc = func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
		return false, fmt.Errorf("auth backend timeout")
	}

	_, err := m.runLinter(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error from authorization failure")
	}
	if !strings.Contains(err.Error(), "auth backend timeout") {
		t.Errorf("expected 'auth backend timeout' in error, got: %v", err)
	}
}
