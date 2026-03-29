// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
)

type defaultResolver struct {
	registry tools.Registry
}

// newToolResolutionService creates a new ToolResolutionService based on a registry.
func newToolResolutionService(registry tools.Registry) ToolResolutionService {
	return &defaultResolver{registry: registry}
}

func (r *defaultResolver) Resolve(call *llm.FunctionCall) (*tools.ToolDeclaration, error) {
	var tool *tools.ToolDeclaration
	var validTools []string
	for _, decl := range r.registry.GetDeclarations() {
		validTools = append(validTools, decl.Name)
		if decl.Name == call.Name {
			tool = decl
		}
	}

	if tool == nil {
		sort.Strings(validTools)
		errorMessage := fmt.Sprintf(
			"Error: Tool %q is not defined. Available tools are: [%s].",
			call.Name, strings.Join(validTools, ", "),
		)

		if suggestion := suggestTool(call.Name, validTools); suggestion != "" {
			errorMessage += fmt.Sprintf(" Did you mean %q?", suggestion)
		}

		errorMessage += " Please check the spelling or use a different tool from the authorized list."

		return nil, fmt.Errorf("%w: %s", llm.ErrTerminal, errorMessage)
	}

	return tool, nil
}

func suggestTool(hallucinated string, validTools []string) string {
	closest := ""
	hallucinatedLower := strings.ToLower(hallucinated)

	// Start with a threshold based on length, max 3.
	// For very short names (<=3), we want distance 1.
	// For medium names, distance 2.
	// For long names, distance 3.
	minDist := 1
	if len(hallucinated) > 6 {
		minDist = 3
	} else if len(hallucinated) > 3 {
		minDist = 2
	}

	for _, tool := range validTools {
		toolLower := strings.ToLower(tool)
		dist := stringsutil.LevenshteinDistance(hallucinatedLower, toolLower)
		if dist <= minDist {
			minDist = dist
			closest = tool
		}
	}
	return closest
}

func resolveTool(reg tools.Registry, call *llm.FunctionCall) (*tools.ToolDeclaration, error) {
	var tool *tools.ToolDeclaration
	var validTools []string
	for _, decl := range reg.GetDeclarations() {
		validTools = append(validTools, decl.Name)
		if decl.Name == call.Name {
			tool = decl
		}
	}

	if tool == nil {
		sort.Strings(validTools)
		errorMessage := fmt.Sprintf(
			"Error: Tool %q is not defined. Available tools are: [%s].",
			call.Name, strings.Join(validTools, ", "),
		)

		if suggestion := suggestTool(call.Name, validTools); suggestion != "" {
			errorMessage += fmt.Sprintf(" Did you mean %q?", suggestion)
		}

		errorMessage += " Please check the spelling or use a different tool from the authorized list."

		return nil, fmt.Errorf("%w: %s", llm.ErrTerminal, errorMessage)
	}

	return tool, nil
}
