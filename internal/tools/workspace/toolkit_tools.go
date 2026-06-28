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
	Description: "CRITICAL: Use this to load specialized tools into your context. Available toolkits: ['git', 'k8s', 'ado', 'jira', 'confluence', 'media', 'network', 'teams']. If a user asks to deploy to Kubernetes, you must call load_toolkit(names=['k8s']) first.",
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

	status := t.resolveToolkitStatus(params.Names, available, info.ActiveToolkits)

	if len(status.loaded) > 0 {
		info.ActiveToolkits = append(info.ActiveToolkits, status.loaded...)
		if err := t.state.SetInfo(ctx, info); err != nil {
			return tools.ToolResult{}, fmt.Errorf("set session info: %w", err)
		}
	}

	responseText := t.formatLoadToolkitResponse(status)
	if responseText == "" {
		return tools.ToolResult{Text: "No toolkits were loaded."}, nil
	}

	return tools.ToolResult{Text: responseText}, nil
}

type toolkitStatus struct {
	loaded        []string
	missing       []string
	alreadyActive []string
}

func (t *persistenceTools) resolveToolkitStatus(requested []string, available []string, active []string) toolkitStatus {
	availableMap := make(map[string]bool)
	for _, tk := range available {
		availableMap[tk] = true
	}

	activeMap := make(map[string]bool)
	for _, tk := range active {
		activeMap[tk] = true
	}

	var status toolkitStatus
	for _, name := range requested {
		if !availableMap[name] {
			status.missing = append(status.missing, name)
			continue
		}
		if activeMap[name] {
			status.alreadyActive = append(status.alreadyActive, name)
			continue
		}
		status.loaded = append(status.loaded, name)
		activeMap[name] = true // prevent duplicates if requested multiple times in the same call
	}
	return status
}

func (t *persistenceTools) formatLoadToolkitResponse(status toolkitStatus) string {
	var sb strings.Builder
	if len(status.loaded) > 0 {
		fmt.Fprintf(&sb, "Successfully loaded toolkits: [%s]. You now have access to those tools. ", strings.Join(status.loaded, ", "))
	}
	if len(status.alreadyActive) > 0 {
		fmt.Fprintf(&sb, "Toolkits already active: [%s]. ", strings.Join(status.alreadyActive, ", "))
	}
	if len(status.missing) > 0 {
		fmt.Fprintf(&sb, "Warning: Unknown toolkits requested and skipped: [%s]. ", strings.Join(status.missing, ", "))
	}
	return strings.TrimSpace(sb.String())
}
