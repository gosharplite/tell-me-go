// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package dev

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
)

func TestRunTestsVulnerability(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true) // Bypass confirmation for tests
	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
		executor:  &mockExecutor{},
	}
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Safe command",
			command: "go help",
			wantErr: false,
		},
		{
			name:    "Command injection via semicolon",
			command: "go test ./internal/config ; echo 'pwned'",
			wantErr: true,
		},
		{
			name:    "Command injection via ampersand",
			command: "go test ./internal/config && echo 'pwned'",
			wantErr: true,
		},
		{
			name:    "Command injection via pipe",
			command: "go test ./internal/config | echo 'pwned'",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command})
			if (err != nil) != tt.wantErr {
				t.Errorf("runTests() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunTests_EdgeCases(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true) // Bypass confirmation for tests
	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
		executor:  &mockExecutor{},
	}
	ctx := context.Background()

	t.Run("Empty command", func(t *testing.T) {
		_, err := m.runTests(ctx, map[string]interface{}{"command": ""})
		if err == nil {
			t.Error("expected error for empty command")
		}
	})

	t.Run("Unauthorized tool", func(t *testing.T) {
		_, err := m.runTests(ctx, map[string]interface{}{"command": "rm -rf /"})
		if err == nil {
			t.Error("expected error for unauthorized tool")
		}
	})

	t.Run("Invalid shlex", func(t *testing.T) {
		_, err := m.runTests(ctx, map[string]interface{}{"command": "go test 'unclosed quote"})
		if err == nil {
			t.Error("expected error for invalid shlex")
		}
	})
}

type mockAuditor struct {
	lastAction string
}

func (m *mockAuditor) LogAudit(label1, val1, label2, val2 string) {
	if label1 == "ACTION" {
		m.lastAction = val1
	}
}
func (m *mockAuditor) SetLogFile(path string) {}

func TestRunTests_Audit(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	auditor := &mockAuditor{}
	sm.Auditor = auditor

	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
		executor:  &mockExecutor{},
	}

	_, err := m.runTests(context.Background(), map[string]interface{}{"command": "go test ./..."})
	if err != nil {
		t.Fatalf("runTests failed: %v", err)
	}

	if auditor.lastAction != "run_tests" {
		t.Errorf("expected audit action 'run_tests', got %q", auditor.lastAction)
	}
}

type mockExecutor struct {
	executeFunc  func(ctx context.Context, name string, args ...string) ([]byte, error)
	lookPathFunc func(file string) (string, error)
}

func (m *mockExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, name, args...)
	}
	return []byte("mock output"), nil
}

func (m *mockExecutor) LookPath(file string) (string, error) {
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return file, nil
}

func TestGetCoverage(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	mock := &mockExecutor{
		executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "test" {
				return []byte("ok"), nil
			}
			if name == "go" && args[0] == "tool" {
				return []byte("total: (statements) 80.0%"), nil
			}
			return nil, nil
		},
	}
	m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}

	res, err := m.getCoverage(context.Background(), map[string]interface{}{"path": "./..."})
	if err != nil {
		t.Fatalf("getCoverage failed: %v", err)
	}
	if !strings.Contains(res.Text, "80.0%") {
		t.Errorf("expected coverage summary, got: %s", res.Text)
	}
}

func TestRunLinter(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)

	t.Run("golangci-lint success", func(t *testing.T) {
		mock := &mockExecutor{
			lookPathFunc: func(file string) (string, error) {
				if file == "golangci-lint" {
					return "/usr/bin/golangci-lint", nil
				}
				return "", fmt.Errorf("not found")
			},
			executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(""), nil
			},
		}
		m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}
		res, err := m.runLinter(context.Background(), nil)
		if err != nil {
			t.Fatalf("runLinter failed: %v", err)
		}
		if !strings.Contains(res.Text, "passed successfully") {
			t.Errorf("expected success message, got: %s", res.Text)
		}
	})

	t.Run("staticcheck success", func(t *testing.T) {
		mock := &mockExecutor{
			lookPathFunc: func(file string) (string, error) {
				if file == "staticcheck" {
					return "/usr/bin/staticcheck", nil
				}
				return "", fmt.Errorf("not found")
			},
			executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(""), nil
			},
		}
		m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}
		res, err := m.runLinter(context.Background(), nil)
		if err != nil {
			t.Fatalf("runLinter failed: %v", err)
		}
		if !strings.Contains(res.Text, "passed successfully") {
			t.Errorf("expected success message, got: %s", res.Text)
		}
	})

	t.Run("linter failure with output", func(t *testing.T) {
		mock := &mockExecutor{
			lookPathFunc: func(file string) (string, error) {
				if file == "golangci-lint" {
					return "/usr/bin/golangci-lint", nil
				}
				return "", fmt.Errorf("not found")
			},
			executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("main.go:1:1: error"), fmt.Errorf("exit status 1")
			},
		}
		m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}
		res, err := m.runLinter(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error for linter failure")
		}
		if !strings.Contains(res.Text, "main.go:1:1: error") {
			t.Errorf("expected linter output, got: %s", res.Text)
		}
	})

	t.Run("no linter found", func(t *testing.T) {
		mock := &mockExecutor{
			lookPathFunc: func(file string) (string, error) {
				return "", fmt.Errorf("not found")
			},
		}
		m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}
		_, err := m.runLinter(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error when no linter found")
		}
	})
}

func TestRunBenchmark(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	mock := &mockExecutor{
		executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("BenchmarkTest 1000 100 ns/op"), nil
		},
	}
	m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}

	res, err := m.runBenchmark(context.Background(), map[string]interface{}{"bench": "Test"})
	if err != nil {
		t.Fatalf("runBenchmark failed: %v", err)
	}
	if !strings.Contains(res.Text, "ns/op") {
		t.Errorf("expected benchmark results, got: %s", res.Text)
	}
}

func TestCheckVulnerabilities(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)

	t.Run("success", func(t *testing.T) {
		mock := &mockExecutor{
			lookPathFunc: func(file string) (string, error) {
				return "/usr/bin/govulncheck", nil
			},
			executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(""), nil
			},
		}
		m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}
		res, err := m.checkVulnerabilities(context.Background(), nil)
		if err != nil {
			t.Fatalf("checkVulnerabilities failed: %v", err)
		}
		if !strings.Contains(res.Text, "No vulnerabilities found") {
			t.Errorf("expected success message, got: %s", res.Text)
		}
	})

	t.Run("vulnerabilities found", func(t *testing.T) {
		mock := &mockExecutor{
			lookPathFunc: func(file string) (string, error) {
				return "/usr/bin/govulncheck", nil
			},
			executeFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("Vulnerability found: GO-2023-XXXX"), fmt.Errorf("exit status 3")
			},
		}
		m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}
		res, err := m.checkVulnerabilities(context.Background(), nil)
		if err == nil {
			t.Fatal("checkVulnerabilities should return error if vulnerabilities are found")
		}
		if !strings.Contains(res.Text, "GO-2023-XXXX") {
			t.Errorf("expected vulnerability details, got: %s", res.Text)
		}
	})
}

func TestGoTidy(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	mock := &mockExecutor{}
	m := &devManager{sm: sm, validator: framework.NewCommandValidator(sm), executor: mock}

	res, err := m.goTidy(context.Background(), nil)
	if err != nil {
		t.Fatalf("goTidy failed: %v", err)
	}
	if !strings.Contains(res.Text, "Success") {
		t.Errorf("expected success message, got: %s", res.Text)
	}
}
