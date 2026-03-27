// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "context"

type approvedToolsKey struct{}
type currentToolKey struct{}

// WithApprovedTools returns a new context with the list of approved tool names injected.
func WithApprovedTools(ctx context.Context, tools []string) context.Context {
	approvedMap := make(map[string]bool, len(tools))
	for _, tool := range tools {
		approvedMap[tool] = true
	}
	return context.WithValue(ctx, approvedToolsKey{}, approvedMap)
}

// WithCurrentTool returns a new context with the current tool name injected.
func WithCurrentTool(ctx context.Context, toolName string) context.Context {
	return context.WithValue(ctx, currentToolKey{}, toolName)
}

// IsCurrentToolApproved returns true if the current tool in the context is in the approved list.
func IsCurrentToolApproved(ctx context.Context) bool {
	currentTool, ok := ctx.Value(currentToolKey{}).(string)
	if !ok {
		return false
	}

	approvedMap, ok := ctx.Value(approvedToolsKey{}).(map[string]bool)
	if !ok {
		return false
	}

	return approvedMap[currentTool]
}
