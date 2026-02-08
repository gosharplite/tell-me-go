// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code"
	"github.com/gosharplite/tell-me-go/internal/tools/dev"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/media"
	"github.com/gosharplite/tell-me-go/internal/tools/network"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
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
	gateway tools.AgentGateway,
) {
	workspace.Register(r, sm, &tools.RealExecutor{})
	framework.RegisterState(r, sm, outputDir)
	framework.RegisterPolicy(r, sm)
	framework.RegisterMetrics(r, sm, logFile, model, mode, pricingOverrides)
	dev.Register(r, sm)
	dev.RegisterRelease(r, sm)
	code.Register(r, sm)
	network.Register(r, sm)
	media.Register(r, sm, gateway)

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
