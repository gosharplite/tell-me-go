// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var loadToolkitDef = &tools.ToolDeclaration{
	Name:        "load_toolkit",
	Description: "CRITICAL: Use this to load specialized tools into your context. Available toolkits: ['git', 'k8s', 'ado', 'jira']. If a user asks to deploy to Kubernetes, you must call load_toolkit(names=['k8s']) first.",
	Parameters: &tools.Schema{
		Type: "OBJECT",
		Properties: map[string]*tools.Schema{
			"names": {
				Type:  "ARRAY",
				Items: &tools.Schema{Type: "STRING"},
			},
		},
		Required: []string{"names"},
	},
	RequiresConsent: false, // The AI should be able to load tools without prompting the user
}

// handleLoadToolkit manages the addition of new toolsets to the current session context.
func (t *persistenceTools) handleLoadToolkit(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Names []string `json:"names"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	info := t.state.GetInfo()
	available := t.reg.ListAvailableToolkits()
	availableMap := make(map[string]bool)
	for _, tk := range available {
		availableMap[tk] = true
	}

	activeMap := make(map[string]bool)
	for _, tk := range info.ActiveToolkits {
		activeMap[tk] = true
	}

	var loaded []string
	var missing []string
	var alreadyActive []string

	for _, name := range params.Names {
		if !availableMap[name] {
			missing = append(missing, name)
			continue
		}
		if activeMap[name] {
			alreadyActive = append(alreadyActive, name)
			continue
		}
		info.ActiveToolkits = append(info.ActiveToolkits, name)
		activeMap[name] = true
		loaded = append(loaded, name)
	}

	if len(loaded) > 0 {
		t.state.SetInfo(info)
	}

	var sb strings.Builder
	if len(loaded) > 0 {
		sb.WriteString(fmt.Sprintf("Successfully loaded toolkits: [%s]. You now have access to those tools. ", strings.Join(loaded, ", ")))
	}
	if len(alreadyActive) > 0 {
		sb.WriteString(fmt.Sprintf("Toolkits already active: [%s]. ", strings.Join(alreadyActive, ", ")))
	}
	if len(missing) > 0 {
		sb.WriteString(fmt.Sprintf("Warning: Unknown toolkits requested and skipped: [%s]. ", strings.Join(missing, ", ")))
	}

	if sb.Len() == 0 {
		return tools.ToolResult{Text: "No toolkits were loaded."}, nil
	}

	return tools.ToolResult{Text: strings.TrimSpace(sb.String())}, nil
}
