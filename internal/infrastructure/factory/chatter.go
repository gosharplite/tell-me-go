// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package factory

import (
	stdctx "context"
	"fmt"

	agent_executor "github.com/gosharplite/tell-me-go/internal/agent/executor"
	agent_orchestration "github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/llmcoord"
	"github.com/gosharplite/tell-me-go/internal/domain/monitoring"
	domain_orchestration "github.com/gosharplite/tell-me-go/internal/domain/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
)

// NewChatter builds the object graph for the orchestration layer.
func NewChatter(ctx stdctx.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
	telemetry.RegisterTraceSubscriber(deps.GetEventBus(), cfg.LogPath)

	summarizer := infra_llm.NewSummarizer(deps.GetGateway(), deps.GetEventBus())

	// 1. Prepare specialized domain service dependencies.
	strategy := agent_orchestration.NewContextStrategy(agent_orchestration.NewHeuristicTokenCounter(deps.GetRegistry()), deps.GetEventBus())
	factory := &agent_orchestration.PipelineFactory{
		Registry:   deps.GetRegistry(),
		History:    deps.GetHistoryManager(),
		Summarizer: summarizer,
		Estimator:  strategy,
		Events:     deps.GetEventBus(),
	}
	ctxManager := agent_orchestration.NewContextManager(strategy, deps.GetHistoryManager(), deps.GetEventBus(), factory)

	toolExec, err := agent_executor.NewToolExecutor(deps.GetRegistry(), deps.GetSecurityManager(), deps.GetEventBus(), &agent_executor.TelemetryLogger{})
	if err != nil {
		return nil, fmt.Errorf("failed to create tool executor: %w", err)
	}

	// 2. Instantiate the four new domain services wrapping robust implementations.
	ctxPrep := agent_orchestration.NewContextPrepAdapter(ctxManager)
	execService := agent_executor.NewExecutionAdapter(toolExec)

	llmCoord := llmcoord.NewService(
		llmcoord.WithGateway(deps.GetGateway()),
		llmcoord.WithStreamHandler(func(ctx stdctx.Context, stream <-chan *llm.Content) {
			_ = deps.GetEventBus().Publish(ctx, events.ResponseStreamEvent{Context: ctx, Stream: stream})
		}),
	)

	monitor := monitoring.NewService(
		monitoring.WithTracker(deps.GetTracker()),
		monitoring.WithEventBus(deps.GetEventBus()),
	)

	// 3. Register internal tools (e.g., summarize_history)
	agent_orchestration.RegisterInternal(deps.GetRegistry(), ctxManager)

	// 4. Return the new ChatterFacade injected with the domain services.
	return domain_orchestration.NewChatterFacade(
		domain_orchestration.WithContextPrep(ctxPrep),
		domain_orchestration.WithExecution(execService),
		domain_orchestration.WithLLMCoord(llmCoord),
		domain_orchestration.WithMonitor(monitor),
		domain_orchestration.WithEventBus(deps.GetEventBus()),
		domain_orchestration.WithRegistry(deps.GetRegistry()),
		domain_orchestration.WithHistory(deps.GetHistoryManager()),
		domain_orchestration.WithProvider(cfg.ProviderName),
		domain_orchestration.WithModel(cfg.Model),
	), nil
}
