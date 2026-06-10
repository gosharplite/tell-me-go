// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"fmt"
	"time"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// heartbeatHooks allows tests to observe or inject faults into the ticker loop.
// In production, a no-op implementation is used.
type heartbeatHooks interface {
	onTick()
}

// prodHeartbeatHooks is the production no-op.
type prodHeartbeatHooks struct{}

func (prodHeartbeatHooks) onTick() {}

// InternalTools provides tool wrappers that interact with agent services.
type InternalTools struct {
	ctxManager *sessctx.Manager
	logger     ports.Logger
	hooks      heartbeatHooks // prodHeartbeatHooks in production; test mocks in tests
}

// NewInternalTools creates a new InternalTools provider.
func NewInternalTools(cm *sessctx.Manager, logger ports.Logger) *InternalTools {
	if logger == nil {
		logger = &ports.NoOpLogger{}
	}
	return &InternalTools{
		ctxManager: cm,
		logger:     logger,
		hooks:      prodHeartbeatHooks{},
	}
}

// emitHeartbeats sends periodic heartbeats until the done channel is closed.
func (t *InternalTools) emitHeartbeats(done <-chan struct{}, hb chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			t.logger.Error("panic in summarize history background drainer: %v", r)
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			t.hooks.onTick()
			if hb != nil {
				select {
				case hb <- struct{}{}:
				default:
				}
			}
		}
	}
}

// SummarizeHistory wraps ContextManager.SummarizeRange as a tool.
func (t *InternalTools) SummarizeHistory(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Turns int    `json:"turns"`
		Focus string `json:"focus"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Turns <= 0 {
		return tools.ToolResult{}, fmt.Errorf("invalid 'turns' parameter: must be > 0")
	}

	// Emit heartbeat while waiting for the slow summarization process
	done := make(chan struct{})
	defer close(done)
	go t.emitHeartbeats(done, hb)

	res, metrics, err := t.ctxManager.SummarizeRange(ctx, params.Turns, params.Focus)
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
		Action string `json:"action"`
		Index  int    `json:"index"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Index < 0 {
		return tools.ToolResult{}, fmt.Errorf("invalid 'index' parameter: must be >= 0")
	}
	var pinned bool

	switch params.Action {
	case "pin":
		pinned = true
	case "unpin":
		pinned = false
	default:
		return tools.ToolResult{}, fmt.Errorf("unsupported action: %s", params.Action)
	}

	if err := t.ctxManager.History.SetPinned(ctx, params.Index, pinned); err != nil {
		return tools.ToolResult{}, err
	}

	status := "unpinned"
	if pinned {
		status = "pinned"
	}
	return tools.ToolResult{Text: fmt.Sprintf("turn %d has been successfully %s", params.Index, status)}, nil
}

// RegisterInternal registers the internal tools with the provided registrar.
func RegisterInternal(r tools.ToolRegistrar, cm *sessctx.Manager, logger ports.Logger) error {
	it := NewInternalTools(cm, logger)

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "summarize_history",
		Description: "Summarizes a specified number of older conversation turns to free up context space.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"turns": {
					Type:        "INTEGER",
					Description: "The number of turns (user+model pairs) to summarize from the beginning of history.",
				},
				"focus": {
					Type:        "STRING",
					Description: "Optional: Specific aspects to focus on in the summary (e.g., 'architecture decisions').",
				},
			},
			Required: []string{"turns"},
		},
	}, it.SummarizeHistory, tools.ToolOptions{
		LongRunning: true,
		Serial:      true,
	}); err != nil {
		return fmt.Errorf("register summarize_history: %w", err)
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
					Type:        "INTEGER",
					Description: "The 0-based index of the turn to manage.",
				},
			},
			Required: []string{"action", "index"},
		},
	}, it.ManageHistory); err != nil {
		return fmt.Errorf("register manage_history: %w", err)
	}
	return nil
}
