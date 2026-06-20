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
	publishInferenceStarted(ctx, Turn)

	defer func() {
		publishResponseDetached(ctx, Turn, respContent)
	}()

	activeToolkits := resolveActiveTools(Turn)

	var activeTools []*tools.ToolDeclaration
	if len(activeToolkits) > 0 {
		activeTools = Turn.Registry.GetDeclarationsByToolkits(activeToolkits)
	} else {
		activeTools = Turn.Registry.GetCoreDeclarations()
	}

	respContent, metrics, err = Turn.Gateway.Generate(ctx, Turn.State.PreparedHistory, activeTools, Turn.CtxManager.History.GetResolver())
	return respContent, metrics, validateResponse(respContent, err)
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

// resolveActiveTools returns the active toolkit names from the Turn's session,
// or nil if the session provider or context manager is unavailable.
func resolveActiveTools(Turn *Turn) []string {
	if Turn == nil || Turn.CtxManager == nil || Turn.CtxManager.SessionProvider == nil {
		return nil
	}
	return Turn.CtxManager.SessionProvider.GetInfo().ActiveToolkits
}

// validateResponse returns ErrLogic if the gateway returned nil content
// without an error (a protocol violation). Otherwise it passes through
// the original error, which may be nil.
func validateResponse(content *llm.Content, err error) error {
	if err == nil && content == nil {
		return NewAgentError(ErrLogic, "api returned nil content", nil)
	}
	return err
}

// publishInferenceStarted fires an InferenceStartedEvent on a best-effort basis.
// Errors are logged but never returned — the inference must proceed regardless
// of whether the UI notification succeeds.
func publishInferenceStarted(ctx context.Context, Turn *Turn) {
	if err := events.SafePublish(ctx, Turn.Events, events.InferenceStartedEvent{Model: Turn.Model}); err != nil {
		Turn.getLogger().Error("Failed to publish InferenceStartedEvent; UI may not show inference status", "error", err)
	}
}

// publishResponseDetached publishes the final response on a context detached
// from the caller's. This guarantees the UI always receives the stop signal
// even when the caller's context is cancelled (e.g., due to a timeout).
// A nil respContent is replaced with a "model" role sentinel so the UI can
// always render a valid closing frame.
func publishResponseDetached(ctx context.Context, Turn *Turn, respContent *llm.Content) {
	safeContent := respContent
	if safeContent == nil {
		safeContent = &llm.Content{Role: "model"}
	}
	stopCtx := context.WithoutCancel(ctx)
	if err := events.SafePublish(stopCtx, Turn.Events, events.ResponseEvent{Content: safeContent}); err != nil {
		Turn.getLogger().Error("Failed to publish ResponseEvent; UI spinner may hang", "error", err)
	}
}
