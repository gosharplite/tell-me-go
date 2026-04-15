// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import (
	"context"
	"time"
)

// Component represents a logical part of the system that can be health-checked.
type Component string

const (
	// CompPersistence represents the storage/database layer.
	CompPersistence Component = "persistence"
	// CompLLMProvider represents the AI model/provider layer.
	CompLLMProvider Component = "llm"
	// CompToolchain represents the local development tools and environment.
	CompToolchain Component = "toolchain"
)

// HealthStatus represents the current state of a component or the whole system.
type HealthStatus string

const (
	// StatusHealthy indicates the component is fully operational.
	StatusHealthy HealthStatus = "healthy"
	// StatusDegraded indicates the component is operational but with performance or non-critical issues.
	StatusDegraded HealthStatus = "degraded"
	// StatusUnhealthy indicates the component is failing and non-functional.
	StatusUnhealthy HealthStatus = "unhealthy"
)

// ComponentReport encapsulates the diagnostic result for a single system component.
type ComponentReport struct {
	Component Component    `json:"component"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message"`
	Details   any          `json:"details,omitempty"`
	Error     error        `json:"-"`
}

// HealthReport provides a consolidated view of the overall system health.
type HealthReport struct {
	OverallStatus HealthStatus                  `json:"overall_status"`
	Components    map[Component]ComponentReport `json:"components"`
	Timestamp     time.Time                     `json:"timestamp"`
}

// HealthChecker is the interface for individual component health providers.
type HealthChecker interface {
	// Check performs a diagnostic check on a specific system component.
	Check(ctx context.Context) (*ComponentReport, error)
}

// HealthCheckManager coordinates multiple HealthCheckers to provide system-wide diagnostics.
type HealthCheckManager interface {
	// CheckAll aggregates health reports from all registered components.
	CheckAll(ctx context.Context) (*HealthReport, error)
	// CheckComponent performs a targeted health check for a single component.
	CheckComponent(ctx context.Context, comp Component) (*ComponentReport, error)
}
