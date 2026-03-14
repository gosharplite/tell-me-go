// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
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

func setupDevManager(t *testing.T) (*devManager, *mockDevExecutor, *security.SecurityManager) {
	t.Helper()
	interactor := &security.MockInteractor{}
	sm := security.NewSecurityManager(interactor)
	sm.SetBypassActive(true)
	sm.RegisterSafePath(".")
	executor := &mockDevExecutor{}
	m := &devManager{
		sm:             sm,
		validator:      security.NewCommandValidator(sm, interactor),
		executor:       executor,
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
			wantErr:    true,
		},
		{
			name:       "Execution failure no output",
			executeErr: errors.New("something went wrong"),
			wantSubstr: "govulncheck failed",
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

			res, err := m.checkVulnerabilities(context.Background(), nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkVulnerabilities() error = %v, wantErr %v", err, tt.wantErr)
			}
			fullOutput := res.Text
			if err != nil {
				fullOutput += " " + err.Error()
			}
			if !strings.Contains(fullOutput, tt.wantSubstr) {
				t.Errorf("expected substring %q, got res.Text=%q, err=%v", tt.wantSubstr, res.Text, err)
			}
		})
	}
}

func setupCoverageMock(t *testing.T, m *devManager, executor *mockDevExecutor, executeOut string, executeErr error, summaryOut string, summaryErr error, tempErr error) {
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "go" && len(args) > 1 {
			if args[0] == "test" {
				return []byte(executeOut), executeErr
			}
			if args[1] == "cover" {
				return []byte(summaryOut), summaryErr
			}
		}
		return nil, nil
	}
	m.createTempFile = func(dir, pattern string) (*os.File, error) {
		if tempErr != nil {
			return nil, tempErr
		}
		f, err := os.CreateTemp(dir, pattern)
		if err == nil {
			t.Cleanup(func() { os.Remove(f.Name()) })
		}
		return f, err
	}
}

func TestGetCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		executeOut string
		executeErr error
		summaryOut string
		summaryErr error
		tempErr    error
		wantSubstr string
		wantErr    bool
	}{
		{
			name:       "Success with 100% coverage",
			executeOut: "ok  	github.com/gosharplite/tell-me-go	0.100s	coverage: 100.0% of statements",
			summaryOut: "total:			(statements)		100.0%",
			wantSubstr: "100.0%",
		},
		{
			name:       "Success with partial coverage",
			executeOut: "ok  	github.com/gosharplite/tell-me-go	0.100s	coverage: 85.0% of statements",
			summaryOut: "github.com/gosharplite/tell-me-go/internal/tools/go:35:	Register		100.0%\ntotal:			(statements)		85.0%",
			wantSubstr: "85.0%",
		},
		{
			name:       "Failure due to test errors",
			executeOut: "FAIL",
			executeErr: errors.New("exit status 1"),
			wantSubstr: "tests failed",
			wantErr:    true,
		},
		{
			name:       "Failure due to missing package",
			executeOut: "can't load package",
			executeErr: errors.New("exit status 1"),
			wantSubstr: "tests failed",
			wantErr:    true,
		},
		{
			name:       "Summary failure",
			executeOut: "ok",
			summaryErr: errors.New("failed to run go tool cover"),
			wantSubstr: "failed to generate coverage summary",
			wantErr:    true,
		},
		{
			name:       "Temp file failure",
			tempErr:    errors.New("failed to create temp file"),
			wantSubstr: "failed to create temp file",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, _ := setupDevManager(t)
			setupCoverageMock(t, m, executor, tt.executeOut, tt.executeErr, tt.summaryOut, tt.summaryErr, tt.tempErr)

			res, err := m.getCoverage(context.Background(), nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantSubstr)
			} else {
				require.NoError(t, err)
				assert.Contains(t, res.Text, tt.wantSubstr)
			}
		})
	}
}

func TestGoTidy(t *testing.T) {
	t.Parallel()
	m, _, _ := setupDevManager(t)

	res, err := m.goTidy(context.Background(), nil)
	if err != nil {
		t.Errorf("goTidy() unexpected error = %v", err)
	}
	if !strings.Contains(res.Text, "Success") {
		t.Errorf("expected Success in result, got %q", res.Text)
	}
}

func TestRunBenchmark(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("BenchmarkResult"), nil
	}

	res, err := m.runBenchmark(context.Background(), map[string]interface{}{"path": "./...", "bench": "BenchmarkFoo"})
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
			wantSubstr: "problem at line 1",
			wantErr:    true,
		},
		{
			name:       "no linter found",
			lookPath:   "none",
			wantErr:    true,
			wantSubstr: "no supported linter found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, executor, _ := setupDevManager(t)
			executor.lookPathFunc = func(file string) (string, error) {
				if file == tt.lookPath {
					return "/usr/bin/" + file, nil
				}
				return "", errors.New("not found")
			}
			executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(tt.executeOut), tt.executeErr
			}

			res, err := m.runLinter(context.Background(), nil)
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

			_, err := m.goTidy(context.Background(), nil)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestRunBenchmark_Error(t *testing.T) {
	t.Parallel()
	m, executor, _ := setupDevManager(t)
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("error output"), errors.New("benchmark failed")
	}

	_, err := m.runBenchmark(context.Background(), nil)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestRunTests(t *testing.T) {
	t.Parallel()
	m, executor, sm := setupDevManager(t)
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("PASS"), nil
	}

	res, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."})
	if err != nil {
		t.Errorf("runTests() unexpected error = %v", err)
	}
	if !strings.Contains(res.Text, "PASS") {
		t.Errorf("expected PASS in result, got %q", res.Text)
	}

	interactor := sm.GetInteractor().(*security.MockInteractor)
	found := false
	for _, w := range interactor.Warns {
		if strings.Contains(w, "[Tool Action] Running Tests") {
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
			_, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command})
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

	_, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestNewDevManager(t *testing.T) {
	t.Parallel()
	interactor := &security.MockInteractor{}
	sm := security.NewSecurityManager(interactor)
	validator := security.NewCommandValidator(sm, interactor)
	m := newDevManager(sm, validator)
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
