// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolchain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockToolchainExecutor struct {
	lookPathFunc       func(file string) (string, error)
	outputFunc         func(ctx context.Context, name string, args ...string) ([]byte, error)
	combinedOutputFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockToolchainExecutor) LookPath(file string) (string, error) {
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return "", fmt.Errorf("lookPath not implemented")
}

func (m *mockToolchainExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.outputFunc != nil {
		return m.outputFunc(ctx, name, args...)
	}
	return nil, fmt.Errorf("output not implemented")
}

func (m *mockToolchainExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.combinedOutputFunc != nil {
		return m.combinedOutputFunc(ctx, name, args...)
	}
	return nil, fmt.Errorf("combinedOutput not implemented")
}

func TestToolchainHealthChecker_Check(t *testing.T) {
	ctx := context.Background()

	t.Run("Healthy", func(t *testing.T) {
		mock := &mockToolchainExecutor{
			lookPathFunc: func(file string) (string, error) {
				return "/usr/bin/" + file, nil
			},
			outputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte(name + " version 1.0.0"), nil
			},
		}

		checker := NewToolchainHealthChecker(mock, []string{"git", "go"}, []string{"make"})
		report, err := checker.Check(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.Status != ports.StatusHealthy {
			t.Errorf("expected StatusHealthy, got %s: %s", report.Status, report.Message)
		}

		details := report.Details.(map[string]any)
		binaries := details["binaries"].(map[string]BinaryInfo)

		if len(binaries) != 3 {
			t.Errorf("expected 3 binaries, got %d", len(binaries))
		}

		if binaries["git"].Path != "/usr/bin/git" {
			t.Errorf("expected path /usr/bin/git, got %s", binaries["git"].Path)
		}

		if !strings.Contains(binaries["git"].Version, "git version") {
			t.Errorf("expected version to contain 'git version', got %s", binaries["git"].Version)
		}
	})

	t.Run("Unhealthy_MissingRequired", func(t *testing.T) {
		mock := &mockToolchainExecutor{
			lookPathFunc: func(file string) (string, error) {
				if file == "git" {
					return "", fmt.Errorf("not found")
				}
				return "/usr/bin/" + file, nil
			},
			outputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("version"), nil
			},
		}

		checker := NewToolchainHealthChecker(mock, []string{"git", "go"}, []string{"make"})
		report, _ := checker.Check(ctx)

		if report.Status != ports.StatusUnhealthy {
			t.Errorf("expected StatusUnhealthy, got %s", report.Status)
		}
		if !strings.Contains(report.Message, "git") {
			t.Errorf("expected message to mention 'git', got %s", report.Message)
		}
	})

	t.Run("Degraded_MissingOptional", func(t *testing.T) {
		mock := &mockToolchainExecutor{
			lookPathFunc: func(file string) (string, error) {
				if file == "make" {
					return "", fmt.Errorf("not found")
				}
				return "/usr/bin/" + file, nil
			},
			outputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("version"), nil
			},
		}

		checker := NewToolchainHealthChecker(mock, []string{"git", "go"}, []string{"make"})
		report, _ := checker.Check(ctx)

		if report.Status != ports.StatusDegraded {
			t.Errorf("expected StatusDegraded, got %s", report.Status)
		}
		if !strings.Contains(report.Message, "make") {
			t.Errorf("expected message to mention 'make', got %s", report.Message)
		}
	})

	t.Run("Unhealthy_ExecutionFailed", func(t *testing.T) {
		mock := &mockToolchainExecutor{
			lookPathFunc: func(file string) (string, error) {
				return "/usr/bin/" + file, nil
			},
			outputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name == "git" {
					return nil, fmt.Errorf("exec error")
				}
				return []byte("version"), nil
			},
		}

		checker := NewToolchainHealthChecker(mock, []string{"git", "go"}, []string{"make"})
		report, _ := checker.Check(ctx)

		if report.Status != ports.StatusUnhealthy {
			t.Errorf("expected StatusUnhealthy, got %s", report.Status)
		}
		if !strings.Contains(report.Message, "git") {
			t.Errorf("expected message to mention 'git', got %s", report.Message)
		}
	})
}
