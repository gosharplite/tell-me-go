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

	t.Run("OverallHealthy", func(t *testing.T) {
		checkers := map[ports.Component]ports.HealthChecker{
			ports.CompPersistence: &mockChecker{
				report: &ports.ComponentReport{Status: ports.StatusHealthy},
			},
			ports.CompLLMProvider: &mockChecker{
				report: &ports.ComponentReport{Status: ports.StatusHealthy},
			},
		}

		m := NewHealthCheckManager(checkers)
		report, err := m.CheckAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.OverallStatus != ports.StatusHealthy {
			t.Errorf("expected StatusHealthy, got %s", report.OverallStatus)
		}
		if len(report.Components) != 2 {
			t.Errorf("expected 2 components, got %d", len(report.Components))
		}
	})

	t.Run("OverallDegraded", func(t *testing.T) {
		checkers := map[ports.Component]ports.HealthChecker{
			ports.CompPersistence: &mockChecker{
				report: &ports.ComponentReport{Status: ports.StatusHealthy},
			},
			ports.CompLLMProvider: &mockChecker{
				report: &ports.ComponentReport{Status: ports.StatusDegraded},
			},
		}

		m := NewHealthCheckManager(checkers)
		report, err := m.CheckAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.OverallStatus != ports.StatusDegraded {
			t.Errorf("expected StatusDegraded, got %s", report.OverallStatus)
		}
	})

	t.Run("OverallUnhealthy", func(t *testing.T) {
		checkers := map[ports.Component]ports.HealthChecker{
			ports.CompPersistence: &mockChecker{
				report: &ports.ComponentReport{Status: ports.StatusHealthy},
			},
			ports.CompLLMProvider: &mockChecker{
				report: &ports.ComponentReport{Status: ports.StatusUnhealthy},
			},
		}

		m := NewHealthCheckManager(checkers)
		report, err := m.CheckAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.OverallStatus != ports.StatusUnhealthy {
			t.Errorf("expected StatusUnhealthy, got %s", report.OverallStatus)
		}
	})

	t.Run("CheckerErrorHandling", func(t *testing.T) {
		checkers := map[ports.Component]ports.HealthChecker{
			ports.CompPersistence: &mockChecker{
				err: errors.New("fatal"),
			},
		}

		m := NewHealthCheckManager(checkers)
		report, err := m.CheckAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if report.OverallStatus != ports.StatusUnhealthy {
			t.Errorf("expected StatusUnhealthy, got %s", report.OverallStatus)
		}
		if report.Components[ports.CompPersistence].Status != ports.StatusUnhealthy {
			t.Errorf("expected component StatusUnhealthy, got %s", report.Components[ports.CompPersistence].Status)
		}
	})
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
