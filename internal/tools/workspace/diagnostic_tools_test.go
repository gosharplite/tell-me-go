// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockHealthCheckManager struct {
	report *ports.HealthReport
	err    error
}

func (m *mockHealthCheckManager) CheckAll(ctx context.Context) (*ports.HealthReport, error) {
	return m.report, m.err
}

func (m *mockHealthCheckManager) CheckComponent(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error) {
	return nil, nil
}

func TestDiagnosticTool_CheckSystemHealth(t *testing.T) {
	ctx := context.Background()

	t.Run("HealthyReport", func(t *testing.T) {
		report := &ports.HealthReport{
			OverallStatus: ports.StatusHealthy,
			Components: map[ports.Component]ports.ComponentReport{
				ports.CompPersistence: {
					Component: ports.CompPersistence,
					Status:    ports.StatusHealthy,
					Message:   "db ok",
				},
			},
			Timestamp: time.Now(),
		}
		mock := &mockHealthCheckManager{report: report}
		tool := newDiagnosticTool(mock)

		res, err := tool.checkSystemHealth(ctx, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(res.Text, string(ports.StatusHealthy)) {
			t.Errorf("expected health status in output, got: %s", res.Text)
		}

		var decoded ports.HealthReport
		if err := json.Unmarshal([]byte(res.Text), &decoded); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}

		if decoded.OverallStatus != ports.StatusHealthy {
			t.Errorf("expected OverallStatus healthy, got %s", decoded.OverallStatus)
		}
	})

	t.Run("ManagerError", func(t *testing.T) {
		mock := &mockHealthCheckManager{err: fmt.Errorf("internal failure")}
		tool := newDiagnosticTool(mock)

		_, err := tool.checkSystemHealth(ctx, nil, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "internal failure") {
			t.Errorf("expected error message to contain 'internal failure', got: %v", err)
		}
	})

	t.Run("NotInitialized", func(t *testing.T) {
		tool := newDiagnosticTool(nil)
		_, err := tool.checkSystemHealth(ctx, nil, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not initialized") {
			t.Errorf("expected error message to contain 'not initialized', got: %v", err)
		}
	})
}

func TestDiagnosticTool_CheckSystemHealth_MarshalFailure(t *testing.T) {
	ctx := context.Background()

	// Put a non-serializable value (channel) in Details to trigger marshal error
	report := &ports.HealthReport{
		OverallStatus: ports.StatusHealthy,
		Components: map[ports.Component]ports.ComponentReport{
			ports.CompPersistence: {
				Component: ports.CompPersistence,
				Status:    ports.StatusHealthy,
				Message:   "db ok",
				Details:   make(chan struct{}), // channels are not JSON-serializable
			},
		},
		Timestamp: time.Now(),
	}
	mock := &mockHealthCheckManager{report: report}
	tool := newDiagnosticTool(mock)

	_, err := tool.checkSystemHealth(ctx, nil, nil)
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to serialize health report") {
		t.Errorf("expected 'failed to serialize health report' in error, got: %v", err)
	}
}
