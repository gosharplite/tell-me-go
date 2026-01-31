// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestToolRegistryIntegrity(t *testing.T) {
	sm := NewSecurityManager()
	registry := NewRegistry()

	// Register all tool groups
	RegisterFileSystemTools(registry, sm)
	RegisterIntelligenceTools(registry, sm)
	RegisterSystemTools(registry, sm)
	RegisterGitTools(registry, sm)
	RegisterDevTools(registry, sm)
	RegisterTeamsTools(registry, sm)
	RegisterStateTools(registry, sm, t.TempDir())
	RegisterMetricsTools(registry, sm, "dummy.log", "model", "mode", nil)
	RegisterMediaTools(registry, sm, nil)

	declarations := registry.GetDeclarations()
	if len(declarations) == 0 {
		t.Fatal("No tools registered")
	}

	for _, decl := range declarations {
		t.Run(decl.Name, func(t *testing.T) {
			// 1. Basic Metadata
			if decl.Name == "" {
				t.Error("Tool name is empty")
			}
			if decl.Description == "" {
				t.Errorf("Tool %s missing description", decl.Name)
			}

			// 2. Parameter Validation
			if decl.Parameters != nil {
				if decl.Parameters.Type != "OBJECT" {
					t.Errorf("Tool %s: parameters must be of type OBJECT, got %s", decl.Name, decl.Parameters.Type)
				}

				if decl.Parameters.Properties == nil {
					t.Errorf("Tool %s: parameters specified but properties is nil", decl.Name)
				}

				for propName, prop := range decl.Parameters.Properties {
					validateSchema(t, decl.Name, propName, prop)
				}

				// Check that all required properties actually exist
				for _, req := range decl.Parameters.Required {
					if _, ok := decl.Parameters.Properties[req]; !ok {
						t.Errorf("Tool %s: required property %q not found in Properties", decl.Name, req)
					}
				}
			}
		})
	}
}

func validateSchema(t *testing.T, toolName, propName string, s *types.Schema) {
	if s == nil {
		return
	}
	if s.Type == "" {
		t.Errorf("Tool %s: property %s has empty type", toolName, propName)
	}
	
	// Valid Types according to GenAI spec (subset)
	validTypes := map[string]bool{
		"STRING":  true,
		"NUMBER":  true,
		"INTEGER": true,
		"BOOLEAN": true,
		"ARRAY":   true,
		"OBJECT":  true,
	}
	if !validTypes[s.Type] {
		t.Errorf("Tool %s: property %s has invalid type %q", toolName, propName, s.Type)
	}

	if s.Type == "ARRAY" && s.Items == nil {
		t.Errorf("Tool %s: property %s is an ARRAY but missing Items", toolName, propName)
	}
	if s.Type == "OBJECT" && s.Properties == nil {
		// Some objects might be empty but usually they should have properties in tool definitions
		t.Errorf("Tool %s: property %s is an OBJECT but missing Properties", toolName, propName)
	}
	
	for subPropName, subProp := range s.Properties {
		validateSchema(t, toolName, propName+"."+subPropName, subProp)
	}
	if s.Items != nil {
		validateSchema(t, toolName, propName+".items", s.Items)
	}
}
