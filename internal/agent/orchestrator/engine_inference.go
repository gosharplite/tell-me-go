// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	start := Turn.Clock.Now()
	respContent, metrics, err := p.InvokeModel(ctx, Turn)
	inferenceDuration := Turn.Clock.Now().Sub(start)

	if trace := telemetry.TraceFromContext(ctx); trace != nil {
		trace.InferenceDuration = inferenceDuration
	}

	if respContent != nil {
		p.updateState(Turn, respContent, metrics)
	}

	if err != nil {
		category := llm.ErrTerminal
		if IsTransient(err) {
			category = llm.ErrTransient
		}
		return ProcessResult{}, NewAgentError(category, "inference failed", err)
	}

	return p.routeBasedOnContent(respContent), nil
}

func (p *InferenceStep) InvokeModel(ctx context.Context, Turn *Turn) (respContent *llm.Content, metrics *llm.Metrics, err error) {
	if err := events.SafePublish(ctx, Turn.Events, events.InferenceStartedEvent{Model: Turn.Model}); err != nil {
		Turn.getLogger().Error("Failed to publish InferenceStartedEvent; UI may not show inference status", "error", err)
	}

	defer func() {
		safeContent := respContent
		if safeContent == nil {
			safeContent = &llm.Content{Role: "model"}
		}
		// Detach context to ensure the UI ALWAYS receives the stop signal even on timeout
		stopCtx := context.WithoutCancel(ctx)
		if err := events.SafePublish(stopCtx, Turn.Events, events.ResponseEvent{Content: safeContent}); err != nil {
			Turn.getLogger().Error("Failed to publish ResponseEvent; UI spinner may hang", "error", err)
		}
	}()

	var activeToolkits []string
	if Turn.CtxManager != nil && Turn.CtxManager.SessionProvider != nil {
		activeToolkits = Turn.CtxManager.SessionProvider.GetInfo().ActiveToolkits
	}

	var activeTools []*tools.ToolDeclaration
	if len(activeToolkits) > 0 {
		activeTools = Turn.Registry.GetDeclarationsByToolkits(activeToolkits)
	} else {
		activeTools = Turn.Registry.GetCoreDeclarations()
	}

	respContent, metrics, err = Turn.Gateway.Generate(ctx, Turn.State.PreparedHistory, activeTools, Turn.CtxManager.History.GetResolver())
	if err == nil && respContent == nil {
		return nil, nil, NewAgentError(ErrLogic, "api returned nil content", nil)
	}
	return respContent, metrics, err
}

func (p *InferenceStep) updateState(Turn *Turn, content *llm.Content, metrics *llm.Metrics) {
	Turn.State.Response = content
	Turn.State.Metrics = metrics
	if metrics != nil {
		metrics.Model = Turn.Model
		metrics.Provider = Turn.ProviderName
		Turn.State.Tokens = int(metrics.PromptTokens)
	}
	Turn.State.HasToolCalls = p.HasToolCalls(content)
	if Turn.State.HasToolCalls {
		// Preallocate capacity based on the number of parts in the response
		Turn.State.ToolReasons = make([]string, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				if reason, ok := part.FunctionCall.Args["reason"].(string); ok && reason != "" {
					Turn.State.ToolReasons = append(Turn.State.ToolReasons, reason)
				}
			}
		}
	}
}

func (p *InferenceStep) routeBasedOnContent(content *llm.Content) ProcessResult {
	if p.HasToolCalls(content) {
		return ProcessResult{NextPhase: PhaseExecuting}
	}
	return ProcessResult{NextPhase: PhasePersisting}
}

func (p *InferenceStep) HasToolCalls(content *llm.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
