// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

// RegisterAll registers all available tools into the registry.
func RegisterAll(
	r *registry.Registry,
	sm *security.SecurityManager,
	outputDir string,
	logFile string,
	model string,
	mode string,
	pricingOverrides map[string]pricing.ModelPricing,
	client llm.LLMClient,
	assetsDir string,
) {
	workspace.Register(r, sm, &tools.RealExecutor{})
	persistence.RegisterState(r, sm, outputDir)
	security.RegisterPolicy(r, sm)
	telemetry.RegisterMetrics(r, sm, logFile, model, mode, pricingOverrides)
	analysis.Register(r, sm)
	integrations.RegisterAll(r, sm, client, assetsDir)

	r.Register(&tools.ToolDeclaration{
		Name:        "generate_mermaid_diagram",
		Description: "Transform a package dependency graph into Mermaid.js 'graph TD' syntax.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"graph": {
					Type:        "OBJECT",
					Description: "A map where keys are package names and values are lists of dependencies.",
				},
			},
			Required: []string{"graph"},
		},
	}, generateMermaidDiagram)
}
