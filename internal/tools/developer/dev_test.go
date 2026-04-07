// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertions to ensure the infrastructure strictly implements the domain interfaces.
// This creates a hard AST reference to clear dead-code false positives in static analysis tools
// that do not respect //nolint directives.
var _ domain_security.ActionConfirmer = (*security.SecurityManager)(nil)
var _ domain_security.TerminalController = (*security.SecurityManager)(nil)
var _ domain_security.UserInteractor = (*security.MockInteractor)(nil)

type mockDevExecutor struct {
	executeFunc  func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPathFunc func(file string) (string, error)
}

func (m *mockDevExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, name, args...)
	}
	return []byte("success"), nil
}

func (m *mockDevExecutor) LookPath(file string) (string, error) {
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return "/usr/bin/" + file, nil
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

func setupDevManager(t *testing.T) (*devManager, *mockDevExecutor, *security.SecurityManager) {
	t.Helper()
	interactor := &security.MockInteractor{}
	sm := security.NewSecurityManager(interactor)
	sm.SetBypassActive(true)
	sm.RegisterSafePath(".")
	executor := &mockDevExecutor{}
	runner := &mockGoRunner{}
	m := &devManager{
		sm:             sm,
		validator:      security.NewCommandValidator(sm, interactor),
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
			lookPathErr: errors.New("not found"),
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, _ := setupDevManager(t)
			executor.lookPathFunc = func(file string) (string, error) {
				return "/usr/bin/" + file, tt.lookPathErr
			}
			executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(tt.executeOut), tt.executeErr
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, _ := setupDevManager(t)
			setupCoverageMock(t, m, tt.executeErr, tt.summaryOut, tt.tempErr, tt.noGoFiles)

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
			wantSubstr: "Linter failed or found issues:",
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
			m, executor, sm := setupDevManager(t)
			if tt.decline {
				sm.SetBypassActive(false)
				sm.GetInteractor().(*security.MockInteractor).Answer = "n"

				m.validator = &mockValidator{
					CommandValidator: m.validator,
					isSafeFunc: func(command string) (bool, string) {
						if strings.Contains(command, "golangci-lint") || strings.Contains(command, "staticcheck") {
							return false, "forced prompt for test"
						}
						return true, ""
					},
				}
			}
			executor.lookPathFunc = func(file string) (string, error) {
				if file == tt.lookPath {
					return "/usr/bin/" + file, nil
				}
				return "", errors.New("not found")
			}
			executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(tt.executeOut), tt.executeErr
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, _ := setupDevManager(t)
			executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if args[0] == tt.cmdFail || (len(args) > 1 && args[1] == tt.cmdFail) {
					return []byte("failed"), tt.executeErr
				}
				return []byte("ok"), nil
			}

			res, err := m.goTidy(context.Background(), nil, nil)
			if err != nil {
				t.Errorf("expected nil error for %s, got %v", tt.name, err)
			}
			displayName := "Go mod tidy"
			if tt.cmdFail == "fmt" {
				displayName = "Go fmt"
			}
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _, _ := setupDevManager(t)
			m.runner.(*mockGoRunner).runBenchmarksFunc = func(ctx context.Context, path string, benchRegex string) (string, error) {
				return tt.executeOut, tt.executeErr
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
	m, executor, sm := setupDevManager(t)
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

	interactor := sm.GetInteractor().(*security.MockInteractor)
	found := false
	for _, w := range interactor.Warns {
		if strings.Contains(w, "[Tool Action] Running test execution") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tool action log in warns, got %v", interactor.Warns)
	}
}

func TestRunTests_Violations(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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

func TestNewDevManager(t *testing.T) {
	t.Parallel()
	interactor := &security.MockInteractor{}
	sm := security.NewSecurityManager(interactor)
	validator := security.NewCommandValidator(sm, interactor)
	runner := &mockGoRunner{}
	m := newDevManager(sm, validator, runner)
	assert.NotNil(t, m)
	assert.NotNil(t, m.executor)
}

func TestAuthorizeAction_Error(t *testing.T) {
	t.Parallel()
	m, _, sm := setupDevManager(t)
	interactor := sm.GetInteractor().(*security.MockInteractor)
	interactor.Err = errors.New("auth failure")
	sm.SetBypassActive(false)

	// Use a command that is NOT safe to trigger interactor call in Authorize
	approved, err := m.authorizeAction(context.Background(), "test", "unauthorized_tool", "detail")
	assert.Error(t, err)
	assert.False(t, approved)
	if err != nil {
		assert.Contains(t, err.Error(), "auth failure")
	}
}

func TestAuthorizeAction_Denied(t *testing.T) {
	t.Parallel()
	m, _, sm := setupDevManager(t)
	interactor := sm.GetInteractor().(*security.MockInteractor)
	interactor.Answer = "n"
	sm.SetBypassActive(false)

	// Use a command that is NOT safe to trigger interactor call in Authorize
	approved, err := m.authorizeAction(context.Background(), "test", "unauthorized_tool", "detail")
	assert.NoError(t, err)
	assert.False(t, approved)
}

func TestResolveLinter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		lookPathMap map[string]error
		wantCmd     string
		wantArgs    []string
		wantErr     bool
	}{
		{
			name: "golangci-lint available",
			lookPathMap: map[string]error{
				"golangci-lint": nil,
			},
			wantCmd:  "golangci-lint",
			wantArgs: []string{"run"},
		},
		{
			name: "staticcheck available (golangci-lint not)",
			lookPathMap: map[string]error{
				"golangci-lint": errors.New("not found"),
				"staticcheck":   nil,
			},
			wantCmd:  "staticcheck",
			wantArgs: []string{"./..."},
		},
		{
			name: "no linter available",
			lookPathMap: map[string]error{
				"golangci-lint": errors.New("not found"),
				"staticcheck":   errors.New("not found"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, _ := setupDevManager(t)
			executor.lookPathFunc = func(file string) (string, error) {
				if err, ok := tt.lookPathMap[file]; ok {
					if err == nil {
						return "/usr/bin/" + file, nil
					}
					return "", err
				}
				return "", errors.New("not found")
			}

			cmd, args, err := m.resolveLinter()
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveLinter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				assert.Equal(t, tt.wantCmd, cmd)
				assert.Equal(t, tt.wantArgs, args)
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
		_, _ = m.executeWithHeartbeat(context.Background(), "test", "cmd", "reason", "echo", []string{"hi"}, hb)
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
		_, err := m.getCoverage(context.Background(), map[string]interface{}{"path": "../../etc/passwd"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "security violation")
	})

	t.Run("runBenchmark sandbox evasion", func(t *testing.T) {
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
