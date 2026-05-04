// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockChecker struct {
	report *ports.ComponentReport
	err    error
}

func (m *mockChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	return m.report, m.err
}

func TestDefaultHealthCheckManager_CheckAll(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		checkers    map[ports.Component]ports.HealthChecker
		wantOverall ports.HealthStatus
	}{
		{
			name: "OverallHealthy",
			checkers: map[ports.Component]ports.HealthChecker{
				ports.CompPersistence: &mockChecker{
					report: &ports.ComponentReport{Status: ports.StatusHealthy},
				},
				ports.CompLLMProvider: &mockChecker{
					report: &ports.ComponentReport{Status: ports.StatusHealthy},
				},
			},
			wantOverall: ports.StatusHealthy,
		},
		{
			name: "OverallDegraded",
			checkers: map[ports.Component]ports.HealthChecker{
				ports.CompPersistence: &mockChecker{
					report: &ports.ComponentReport{Status: ports.StatusHealthy},
				},
				ports.CompLLMProvider: &mockChecker{
					report: &ports.ComponentReport{Status: ports.StatusDegraded},
				},
			},
			wantOverall: ports.StatusDegraded,
		},
		{
			name: "OverallUnhealthy",
			checkers: map[ports.Component]ports.HealthChecker{
				ports.CompPersistence: &mockChecker{
					report: &ports.ComponentReport{Status: ports.StatusHealthy},
				},
				ports.CompLLMProvider: &mockChecker{
					report: &ports.ComponentReport{Status: ports.StatusUnhealthy},
				},
			},
			wantOverall: ports.StatusUnhealthy,
		},
		{
			name: "CheckerErrorHandling",
			checkers: map[ports.Component]ports.HealthChecker{
				ports.CompPersistence: &mockChecker{
					err: errors.New("fatal"),
				},
			},
			wantOverall: ports.StatusUnhealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewHealthCheckManager(tt.checkers)
			report, err := m.CheckAll(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if report.OverallStatus != tt.wantOverall {
				t.Errorf("expected %s, got %s", tt.wantOverall, report.OverallStatus)
			}

			// Component count assertion: report must include every registered checker.
			if len(report.Components) != len(tt.checkers) {
				t.Errorf("expected %d components, got %d", len(tt.checkers), len(report.Components))
			}

			// For error-handling case, verify the failing component
			// is individually marked Unhealthy.
			if tt.name == "CheckerErrorHandling" {
				comp := report.Components[ports.CompPersistence]
				if comp.Status != ports.StatusUnhealthy {
					t.Errorf("expected component %s StatusUnhealthy, got %s", ports.CompPersistence, comp.Status)
				}
			}
		})
	}
}

func TestDefaultHealthCheckManager_CheckComponent(t *testing.T) {
	ctx := context.Background()

	checkers := map[ports.Component]ports.HealthChecker{
		ports.CompPersistence: &mockChecker{
			report: &ports.ComponentReport{Status: ports.StatusHealthy, Message: "ok"},
		},
	}
	m := NewHealthCheckManager(checkers)

	t.Run("Existing", func(t *testing.T) {
		report, err := m.CheckComponent(ctx, ports.CompPersistence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Status != ports.StatusHealthy {
			t.Errorf("expected StatusHealthy, got %s", report.Status)
		}
	})

	t.Run("NonExisting", func(t *testing.T) {
		_, err := m.CheckComponent(ctx, ports.CompToolchain)
		if err == nil {
			t.Fatal("expected error for non-existing component, got nil")
		}
	})
}
