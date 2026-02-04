// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code"
	"github.com/gosharplite/tell-me-go/internal/tools/dev"
	"github.com/gosharplite/tell-me-go/internal/tools/files"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/git"
	"github.com/gosharplite/tell-me-go/internal/tools/media"
	"github.com/gosharplite/tell-me-go/internal/tools/network"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/system"
)

// Registry is a type alias for backward compatibility during refactoring.
// Deprecated: Use registry.Registry directly.
type Registry = registry.Registry

// ToolFunc is a type alias for backward compatibility during refactoring.
// Deprecated: Use registry.ToolFunc directly.
type ToolFunc = registry.ToolFunc

// ToolOptions is a type alias for backward compatibility during refactoring.
// Deprecated: Use registry.ToolOptions directly.
type ToolOptions = registry.ToolOptions

// UnmarshalArgs is a wrapper for backward compatibility during refactoring.
// Deprecated: Use registry.UnmarshalArgs directly.
func UnmarshalArgs(args map[string]interface{}, target interface{}) error {
	return registry.UnmarshalArgs(args, target)
}

// NewRegistry initializes an empty tool registry.
// Deprecated: Use registry.New() directly.
func NewRegistry() *registry.Registry {
	return registry.New()
}

// RegisterAll registers all available tools into the registry.
func RegisterAll(r *registry.Registry, sm *security.SecurityManager, configDir string, version string, gateway tools.AgentGateway) {
	files.Register(r, sm)
	framework.RegisterState(r, sm, configDir)
	framework.RegisterPolicy(r, sm)
	system.Register(r, sm)
	git.Register(r, sm)
	dev.Register(r, sm)
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
	}, GenerateMermaidDiagram)
}
