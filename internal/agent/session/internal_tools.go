// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"fmt"
	"time"

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

// emitHeartbeats sends periodic heartbeats until the done channel is closed.
func emitHeartbeats(done <-chan struct{}, hb chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			// Prevent crashing the tool execution silently
			fmt.Printf("panic in summarize history background drainer: %v\n", r)
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if hb != nil {
				select {
				case hb <- struct{}{}:
				default:
				}
			}
		}
	}
}

// summarizeHistory wraps ContextManager.SummarizeRange as a tool.
func (t *InternalTools) summarizeHistory(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	// Emit heartbeat while waiting for the slow summarization process
	done := make(chan struct{})
	defer close(done)
	go emitHeartbeats(done, hb)

	res, metrics, err := t.ctxManager.SummarizeRange(ctx, targetTurns, params.Focus)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{
		Text:     res,
		Metadata: map[string]interface{}{"metrics": metrics},
	}, nil
}

// ManageHistory manages conversation history by pinning or unpinning specific turns.
func (t *InternalTools) ManageHistory(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

// RegisterInternal registers the internal tools with the provided registrar.
func RegisterInternal(r tools.ToolRegistrar, cm *ContextManager) error {
	it := NewInternalTools(cm)

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "summarize_history",
		Description: "Summarizes a specified number of older conversation turns to free up context space.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"turns": {
					Type:        "NUMBER",
					Description: "The number of turns (user+model pairs) to summarize from the beginning of history.",
				},
				"focus": {
					Type:        "STRING",
					Description: "Optional: Specific aspects to focus on in the summary (e.g., 'architecture decisions').",
				},
			},
			Required: []string{"turns"},
		},
	}, it.summarizeHistory, tools.ToolOptions{
		LongRunning: true,
		Serial:      true,
	}); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "manage_history",
		Description: "Manages conversation history by pinning or unpinning specific turns to protect them from summarization/pruning.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"action": {
					Type:        "STRING",
					Description: "The action to perform: 'pin' or 'unpin'.",
					Enum:        []string{"pin", "unpin"},
				},
				"index": {
					Type:        "NUMBER",
					Description: "The 0-based index of the turn to manage.",
				},
			},
			Required: []string{"action", "index"},
		},
	}, it.ManageHistory); err != nil {
		return err
	}
	return nil
}
