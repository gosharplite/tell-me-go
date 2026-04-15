// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// DefaultHealthCheckManager coordinates system-wide health diagnostics.
type DefaultHealthCheckManager struct {
	checkers map[ports.Component]ports.HealthChecker
}

// NewHealthCheckManager creates a new DefaultHealthCheckManager.
func NewHealthCheckManager(checkers map[ports.Component]ports.HealthChecker) *DefaultHealthCheckManager {
	return &DefaultHealthCheckManager{
		checkers: checkers,
	}
}

// CheckAll executes health checks for all registered components in parallel.
func (m *DefaultHealthCheckManager) CheckAll(ctx context.Context) (*ports.HealthReport, error) {
	report := &ports.HealthReport{
		OverallStatus: ports.StatusHealthy,
		Components:    make(map[ports.Component]ports.ComponentReport),
		Timestamp:     time.Now(),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for comp, checker := range m.checkers {
		wg.Add(1)
		go func(c ports.Component, h ports.HealthChecker) {
			defer wg.Done()

			compReport, err := h.Check(ctx)
			if err != nil {
				// We don't fail the whole check if one component fails to report.
				// We capture it as unhealthy instead.
				compReport = &ports.ComponentReport{
					Component: c,
					Status:    ports.StatusUnhealthy,
					Message:   fmt.Sprintf("Health check failed to execute: %v", err),
					Error:     err,
				}
			}

			mu.Lock()
			report.Components[c] = *compReport
			mu.Unlock()
		}(comp, checker)
	}

	wg.Wait()

	// Heuristic for OverallStatus
	hasUnhealthy := false
	hasDegraded := false
	for _, cr := range report.Components {
		switch cr.Status {
		case ports.StatusUnhealthy:
			hasUnhealthy = true
		case ports.StatusDegraded:
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		report.OverallStatus = ports.StatusUnhealthy
	} else if hasDegraded {
		report.OverallStatus = ports.StatusDegraded
	}

	return report, nil
}

// CheckComponent performs a targeted health check for a single component.
func (m *DefaultHealthCheckManager) CheckComponent(ctx context.Context, comp ports.Component) (*ports.ComponentReport, error) {
	checker, ok := m.checkers[comp]
	if !ok {
		return nil, fmt.Errorf("no health checker registered for component: %s", comp)
	}
	return checker.Check(ctx)
}
