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

var healthyMock = &mockToolchainExecutor{
	lookPathFunc: func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	},
	outputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(name + " version 1.0.0"), nil
	},
}

var missingRequiredMock = &mockToolchainExecutor{
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

var missingOptionalMock = &mockToolchainExecutor{
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

var executionFailedMock = &mockToolchainExecutor{
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

func TestToolchainHealthChecker_Check(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		mock       *mockToolchainExecutor
		wantStatus ports.HealthStatus
		wantMsg    string
		wantBins   int
	}{
		{
			name:       "Healthy",
			mock:       healthyMock,
			wantStatus: ports.StatusHealthy,
			wantBins:   3,
		},
		{
			name:       "Unhealthy_MissingRequired",
			mock:       missingRequiredMock,
			wantStatus: ports.StatusUnhealthy,
			wantMsg:    "git",
		},
		{
			name:       "Degraded_MissingOptional",
			mock:       missingOptionalMock,
			wantStatus: ports.StatusDegraded,
			wantMsg:    "make",
		},
		{
			name:       "Unhealthy_ExecutionFailed",
			mock:       executionFailedMock,
			wantStatus: ports.StatusUnhealthy,
			wantMsg:    "git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewToolchainHealthChecker(tt.mock, []string{"git", "go"}, []string{"make"})
			report, err := checker.Check(ctx)
			
			assertBasicStatus(t, report, err, tt.wantStatus, tt.wantMsg)
			
			if tt.wantBins > 0 {
				assertBinaries(t, report, tt.wantBins)
			}
		})
	}
}

func assertBasicStatus(t *testing.T, report *ports.ComponentReport, err error, wantStatus ports.HealthStatus, wantMsg string) {
	t.Helper()
	if err != nil && wantStatus == ports.StatusHealthy {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != wantStatus {
		t.Errorf("expected Status %s, got %s: %s", wantStatus, report.Status, report.Message)
	}

	if wantMsg != "" && !strings.Contains(report.Message, wantMsg) {
		t.Errorf("expected message to mention '%s', got %s", wantMsg, report.Message)
	}
}

func assertBinaries(t *testing.T, report *ports.ComponentReport, wantBins int) {
	t.Helper()
	details := report.Details.(map[string]any)
	binaries := details["binaries"].(map[string]binaryInfo)

	if len(binaries) != wantBins {
		t.Errorf("expected %d binaries, got %d", wantBins, len(binaries))
	}

	if git, ok := binaries["git"]; ok {
		if git.Path != "/usr/bin/git" {
			t.Errorf("expected path /usr/bin/git, got %s", git.Path)
		}
		if !strings.Contains(git.Version, "git version") {
			t.Errorf("expected version to contain 'git version', got %s", git.Version)
		}
	}
}
