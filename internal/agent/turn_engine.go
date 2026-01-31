// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// TurnEngine manages the "Think -> Act -> Observe" cycle.
type TurnEngine struct {
	ctxManager  *ContextManager
	gateway     gateway.LLMGateway
	executor    *ToolExecutor
	renderer    UIRenderer
	registry    ToolRegistry
	logFile     string
	OnTurnStart func()
}

// NewTurnEngine creates a new TurnEngine.
func NewTurnEngine(gw gateway.LLMGateway, ex *ToolExecutor, cm *ContextManager, r UIRenderer, reg ToolRegistry) *TurnEngine {
	return &TurnEngine{
		gateway:    gw,
		executor:   ex,
		ctxManager: cm,
		renderer:   r,
		registry:   reg,
	}
}

// SetLogFile sets the path for usage logging.
func (e *TurnEngine) SetLogFile(path string) {
	e.logFile = path
}

// Run executes the multi-turn orchestration loop.
func (e *TurnEngine) Run(ctx context.Context, startTime time.Time) error {
	for turn := 0; ; turn++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if e.OnTurnStart != nil {
			e.OnTurnStart()
		}

		_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
		if turn > maxTurns {
			break
		}

		// 1. Prepare Context
		apiContents, tokens, currentTurns, err := e.ctxManager.Prepare(ctx, turn)
		if err != nil {
			return err
		}

		e.logTurnStatus(currentTurns, tokens, nil, false, startTime)

		// 2. Generate Response
		respContent, metrics, err := e.gateway.Generate(ctx, apiContents, e.registry.GetDeclarations(), e.ctxManager.History.GetResolver())
		if err != nil {
			return err
		}

		// 3. Persist Response
		if err := e.ctxManager.History.AddContent(ctx, respContent); err != nil {
			e.renderer.LogSystemMessage(fmt.Sprintf("Failed to persist history entry: %v", err), "warn")
		}

		// 4. Handle Tool Execution
		if err := e.handleToolExecution(ctx, respContent, turn, metrics); err != nil {
			return err
		}

		e.logTurnStatus(currentTurns, tokens, metrics, true, startTime)

		if metrics != nil {
			e.renderer.LogUsage(metrics, e.logFile, startTime)
		}

		if !e.hasToolCalls(respContent) {
			break
		}
	}
	return nil
}

func (e *TurnEngine) handleToolExecution(ctx context.Context, respContent *types.Content, turn int, metrics *types.Metrics) error {
	toolStart := time.Now()
	_, maxToolTurns, _ := e.ctxManager.Strategy.GetLimits()

	toolResponse, err := e.executor.Execute(ctx, respContent, turn, maxToolTurns)
	if err != nil {
		return err
	}

	if toolResponse != nil {
		if err := e.ctxManager.History.AddContent(ctx, toolResponse); err != nil {
			e.renderer.LogSystemMessage(fmt.Sprintf("Failed to persist history entry: %v", err), "warn")
		}
	}

	if metrics != nil {
		metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return nil
}

func (e *TurnEngine) logTurnStatus(currentTurns, tokens int, metrics *types.Metrics, isPost bool, startTime time.Time) {
	maxTokens, _, maxHistTurns := e.ctxManager.Strategy.GetLimits()
	e.renderer.LogTurnStatus(TurnStatus{
		Timestamp:        time.Now(),
		CurrentTurns:     currentTurns,
		MaxHistoryTurns:  maxHistTurns,
		Tokens:           tokens,
		MaxHistoryTokens: maxTokens,
		Metrics:          metrics,
		IsPostCall:       isPost,
		StartTime:        startTime,
	})
}

func (e *TurnEngine) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
