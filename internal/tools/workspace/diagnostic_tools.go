// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// diagnosticTool provides system health diagnostic capabilities to the agent.
type diagnosticTool struct {
	health ports.HealthCheckManager
}

// newDiagnosticTool creates a new diagnosticTool instance.
func newDiagnosticTool(health ports.HealthCheckManager) *diagnosticTool {
	return &diagnosticTool{
		health: health,
	}
}

// checkSystemHealth handles the check_system_health tool call.
func (t *diagnosticTool) checkSystemHealth(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if t.health == nil {
		return tools.ToolResult{}, fmt.Errorf("health check manager is not initialized")
	}

	report, err := t.health.CheckAll(ctx)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to perform system health check: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to serialize health report: %w", err)
	}

	return tools.ToolResult{
		Text: string(data),
	}, nil
}
