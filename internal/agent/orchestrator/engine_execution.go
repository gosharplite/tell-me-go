// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
)

// ExecutionStep executes tools if any.
type ExecutionStep struct{}

func (p *ExecutionStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	if !Turn.State.HasToolCalls {
		return ProcessResult{NextPhase: PhasePersisting}, nil
	}

	var names []string
	if Turn.State.Response != nil {
		names = make([]string, 0, len(Turn.State.Response.Parts))
		for _, part := range Turn.State.Response.Parts {
			if part.FunctionCall != nil {
				names = append(names, part.FunctionCall.Name)
			}
		}
	}
	_ = events.SafePublish(ctx, Turn.Events, events.ToolExecutionStartedEvent{ToolNames: names})

	toolStart := Turn.Clock.Now()

	toolResponse, err := Turn.Executor.Execute(ctx, Turn.State.Response, Turn.Index, Turn.MaxToolTurns)

	if toolResponse != nil {
		Turn.State.ToolResponse = toolResponse
		p.validatePayloadLimits(ctx, Turn)
	}

	if err != nil {
		return ProcessResult{}, p.handleToolExecutionError(err)
	}

	if Turn.State.Metrics != nil {
		Turn.State.Metrics.ToolDuration = Turn.Clock.Now().Sub(toolStart).Seconds()
		if trace := telemetry.TraceFromContext(ctx); trace != nil {
			Turn.State.Metrics.CumulativeToolDuration = trace.CumulativeToolDuration().Seconds()
		}
	}
	return ProcessResult{NextPhase: PhasePersisting}, nil
}

func (p *ExecutionStep) handleToolExecutionError(err error) error {
	category := llm.ErrTerminal
	if IsTransient(err) {
		category = llm.ErrTransient
	}
	return NewAgentError(category, "tool execution failed", err)
}

func (p *ExecutionStep) validatePayloadLimits(ctx context.Context, Turn *Turn) {
	if Turn.State.ToolResponse == nil || Turn.CtxManager == nil || Turn.CtxManager.Strategy == nil {
		return
	}

	limits := Turn.CtxManager.GetLimits()
	if limits.MaxHistoryTokens <= 0 {
		return
	}

	toolTokens := Turn.TokenCounter.Count([]*llm.Content{Turn.State.ToolResponse})
	isTooLarge, instruction := p.checkTokenBudget(Turn, toolTokens, limits)

	if isTooLarge {
		p.handleOversizedPayload(ctx, Turn, toolTokens, instruction)
	}
}

func (p *ExecutionStep) checkTokenBudget(Turn *Turn, toolTokens int, limits events.Limits) (bool, string) {
	// We use the remaining buffer, accounting for the 10% system reservation
	maxAllowed := int(float64(limits.MaxHistoryTokens) * 0.90)

	// Cap individual tool response size to 50% of total limit just in case,
	// AND ensure it doesn't push the total over the cliff.
	if toolTokens > int(float64(limits.MaxHistoryTokens)*0.50) {
		return true, "The individual tool output is too massive. You MUST use precise boundaries (e.g., 'tail_lines', 'max_lines', 'limit', or 'grep'). Summarizing history will not fix this."
	} else if Turn.State.Tokens+toolTokens > maxAllowed {
		return true, "The total conversation context is nearly exhausted. Please call 'summarize_history' first to free up space, then run the tool again."
	}

	return false, ""
}

func (p *ExecutionStep) handleOversizedPayload(ctx context.Context, Turn *Turn, toolTokens int, instruction string) {
	// Delegate mutation to the utility with context-aware instruction
	TruncateOversizedResponse(Turn.State.ToolResponse, toolTokens, instruction)

	evt := events.SystemMessageEvent{
		Message: fmt.Sprintf("Tool output truncated (~%d tokens) to prevent exceeding safety limit.", toolTokens),
		Level:   "error",
	}
	if err := events.SafePublish(ctx, Turn.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			Turn.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}
