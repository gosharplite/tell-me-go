// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package dev

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
)

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
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	sm.RegisterSafePath(".")
	executor := &mockDevExecutor{}
	m := &devManager{
		sm:             sm,
		validator:      framework.NewCommandValidator(sm),
		executor:       executor,
		stderr:         io.Discard,
		createTempFile: os.CreateTemp,
	}
	return m, executor, sm
}

func TestCheckVulnerabilities(t *testing.T) {
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
			executeOut: "",
			executeErr: errors.New("something went wrong"),
			wantSubstr: "govulncheck failed",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			if !strings.Contains(res.Text, tt.wantSubstr) && (err != nil && !strings.Contains(err.Error(), tt.wantSubstr)) {
				t.Errorf("expected substring %q, got res.Text=%q, err=%v", tt.wantSubstr, res.Text, err)
			}
		})
	}
}

func TestGetCoverage(t *testing.T) {
	tests := []struct {
		name       string
		executeOut string
		executeErr error
		summaryOut string
		summaryErr error
		wantSubstr string
		wantErr    bool
	}{
		{
			name:       "Success",
			executeOut: "ok  	github.com/gosharplite/tell-me-go	0.100s	coverage: 85.0% of statements",
			summaryOut: "github.com/gosharplite/tell-me-go/internal/tools/dev/dev.go:35:	Register		100.0%\ntotal:			(statements)		85.0%",
			wantSubstr: "total:",
		},
		{
			name:       "Test failure",
			executeOut: "FAIL",
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
			wantSubstr: "failed to create temp file",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, executor, _ := setupDevManager(t)
			executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "go" && len(args) > 1 && args[0] == "test" {
					return []byte(tt.executeOut), tt.executeErr
				}
				if name == "go" && len(args) > 1 && args[1] == "cover" {
					return []byte(tt.summaryOut), tt.summaryErr
				}
				return []byte(""), nil
			}
			m.createTempFile = func(dir, pattern string) (*os.File, error) {
				if tt.name == "Temp file failure" {
					return nil, errors.New("failed to create temp file")
				}
				f, err := os.CreateTemp(dir, pattern)
				if err == nil {
					t.Cleanup(func() { os.Remove(f.Name()) })
				}
				return f, err
			}

			res, err := m.getCoverage(context.Background(), nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCoverage() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !strings.Contains(res.Text, tt.wantSubstr) {
				t.Errorf("expected substring %q in %q", tt.wantSubstr, res.Text)
			} else if tt.wantErr && !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("expected substring %q in error %v", tt.wantSubstr, err)
			}
		})
	}
}

func TestGoTidy(t *testing.T) {
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
			executeOut: "",
			wantSubstr: "Linter passed successfully",
		},
		{
			name:       "staticcheck success",
			lookPath:   "staticcheck",
			executeOut: "",
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
			if !strings.Contains(res.Text, tt.wantSubstr) && (err != nil && !strings.Contains(err.Error(), tt.wantSubstr)) {
				t.Errorf("expected %q, got res.Text=%q, err=%v", tt.wantSubstr, res.Text, err)
			}
		})
	}
}

func TestGoTidy_Errors(t *testing.T) {
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
	var stderr bytes.Buffer
	m, executor, _ := setupDevManager(t)
	m.stderr = &stderr
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

	if !strings.Contains(stderr.String(), "[Tool Action] Running Tests") {
		t.Errorf("expected tool action log, got %q", stderr.String())
	}
}

func TestRunTests_Violations(t *testing.T) {
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
			_, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command})
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestRunTests_Failure(t *testing.T) {
	m, executor, _ := setupDevManager(t)
	executor.executeFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("FAIL"), errors.New("exit status 1")
	}

	_, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."})
	if err == nil {
		t.Error("expected error, got nil")
	}
}
