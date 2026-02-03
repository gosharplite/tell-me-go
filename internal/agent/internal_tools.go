// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// InternalTools provides tool wrappers that interact with agent services.
type InternalTools struct {
	ctxManager *ContextManager
}

// NewInternalTools creates a new InternalTools provider.
func NewInternalTools(cm *ContextManager) *InternalTools {
	return &InternalTools{ctxManager: cm}
}

// SummarizeHistory wraps ContextManager.SummarizeRange as a tool.
func (t *InternalTools) SummarizeHistory(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Turns float64 `json:"turns"`
		Focus string  `json:"focus"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	targetTurns := int(params.Turns)
	if targetTurns <= 0 {
		return tools.ToolResult{}, fmt.Errorf("invalid 'turns' parameter: must be > 0")
	}

	res, err := t.ctxManager.SummarizeRange(ctx, targetTurns, params.Focus)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: res}, nil
}

// ManageHistory manages conversation history by pinning or unpinning specific turns.
func (t *InternalTools) ManageHistory(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Action string  `json:"action"`
		Index  float64 `json:"index"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	index := int(params.Index)
	var pinned bool

	switch params.Action {
	case "pin":
		pinned = true
	case "unpin":
		pinned = false
	default:
		return tools.ToolResult{}, fmt.Errorf("unsupported action: %s", params.Action)
	}

	if err := t.ctxManager.History.SetPinned(ctx, index, pinned); err != nil {
		return tools.ToolResult{}, err
	}

	status := "unpinned"
	if pinned {
		status = "pinned"
	}
	return tools.ToolResult{Text: fmt.Sprintf("Turn %d has been successfully %s.", index, status)}, nil
}
