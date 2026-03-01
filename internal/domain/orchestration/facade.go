// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// chatterFacade implements the ports.Chatter interface by coordinating specialized domain services.
type chatterFacade struct {
	mu sync.RWMutex

	contextPrep ContextPreparationService
	execution   ExecutionOrchestrator
	llmCoord    LLMCoordinator
	monitor     MonitoringTracker

	bus      events.EventBus
	registry tools.IToolRegistry
	history  ports.HistoryManager

	// Internal state/config
	maxToolTurns int
}

// facadeOption defines a functional option for initializing the chatterFacade.
type facadeOption func(*chatterFacade)

// WithContextPrep sets the context preparation service for the facade.
func WithContextPrep(s ContextPreparationService) facadeOption {
	return func(f *chatterFacade) { f.contextPrep = s }
}

// WithExecution sets the execution orchestrator for the facade.
func WithExecution(s ExecutionOrchestrator) facadeOption {
	return func(f *chatterFacade) { f.execution = s }
}

// WithLLMCoord sets the LLM coordinator for the facade.
func WithLLMCoord(s LLMCoordinator) facadeOption {
	return func(f *chatterFacade) { f.llmCoord = s }
}

// WithMonitor sets the monitoring tracker for the facade.
func WithMonitor(s MonitoringTracker) facadeOption {
	return func(f *chatterFacade) { f.monitor = s }
}

// WithEventBus sets the event bus for the facade.
func WithEventBus(bus events.EventBus) facadeOption {
	return func(f *chatterFacade) { f.bus = bus }
}

// WithRegistry sets the tool registry for the facade.
func WithRegistry(reg tools.IToolRegistry) facadeOption {
	return func(f *chatterFacade) { f.registry = reg }
}

// WithHistory sets the history manager for the facade.
func WithHistory(h ports.HistoryManager) facadeOption {
	return func(f *chatterFacade) { f.history = h }
}

// NewChatterFacade creates a new Chatter implementation using the facade pattern.
func NewChatterFacade(opts ...facadeOption) ports.Chatter {
	f := &chatterFacade{
		maxToolTurns: 10, // Defaults
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Chat runs the multi-turn orchestration loop.
func (f *chatterFacade) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	// Initialize context with user prompt
	if err := f.contextPrep.AddContent(ctx, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to add user prompt: %w", err)
	}

	f.emit(ctx, events.StatusUpdate{Message: "Starting chat...", Level: "info"})

	// Main Turn Loop
	for turn := 0; turn <= f.maxToolTurns; turn++ {
		stop, retry, err := f.executeTurn(ctx, turn)
		if err != nil {
			return err
		}
		if retry {
			turn--
			continue
		}
		if stop {
			break
		}
	}

	return nil
}

func (f *chatterFacade) executeTurn(ctx context.Context, turn int) (stop bool, retry bool, err error) {
	// Guard check for context cancellation
	select {
	case <-ctx.Done():
		return false, false, ctx.Err()
	default:
	}

	// Signal turn start
	f.emit(ctx, events.TurnStarted{Turn: turn, MaxTurns: f.maxToolTurns})

	// Prepare Context
	history, err := f.contextPrep.Prepare(ctx, turn)
	if err != nil {
		f.monitor.RecordError(ctx, err)
		return false, false, err
	}

	// Invoke LLM (Inference)
	response, metrics, err := f.llmCoord.Generate(ctx, history, f.registry.GetDeclarations(), f.history.GetResolver())
	if err != nil {
		if llm.IsTransient(err) {
			f.emit(ctx, events.SystemMessageEvent{Message: fmt.Sprintf("Transient error: %v. Retrying...", err), Level: "warn"})
			return false, true, nil
		}
		f.monitor.RecordError(ctx, err)
		return false, false, err
	}

	// Track Usage and persist response
	_ = f.monitor.TrackUsage(ctx, metrics)
	if err := f.contextPrep.AddContent(ctx, response); err != nil {
		return false, false, fmt.Errorf("failed to persist response: %w", err)
	}

	// Check for tool calls
	if !f.hasToolCalls(response) {
		return true, false, nil
	}

	// Execute Tools
	return f.runTools(ctx, response, turn)
}

func (f *chatterFacade) hasToolCalls(content *llm.Content) bool {
	if content == nil {
		return false
	}
	for _, p := range content.Parts {
		if p.FunctionCall != nil {
			return true
		}
	}
	return false
}

func (f *chatterFacade) runTools(ctx context.Context, response *llm.Content, turn int) (stop bool, retry bool, err error) {
	toolResults, err := f.execution.Execute(ctx, response, turn, f.maxToolTurns)
	if err != nil {
		if llm.IsTransient(err) {
			f.emit(ctx, events.SystemMessageEvent{Message: fmt.Sprintf("Transient tool error: %v. Retrying...", err), Level: "warn"})
			return false, true, nil
		}
		f.monitor.RecordError(ctx, err)
		return false, false, err
	}

	// Persist Tool Results
	if toolResults != nil {
		if err := f.contextPrep.AddContent(ctx, toolResults); err != nil {
			return false, false, fmt.Errorf("failed to persist tool results: %w", err)
		}
	}
	return false, false, nil
}

func (f *chatterFacade) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.maxToolTurns = toolTurns
	return nil
}

func (f *chatterFacade) SetTieredThreshold(ctx context.Context, threshold int) error {
	return nil
}

func (f *chatterFacade) Subscribe(sub func(events.Event)) {
	if f.bus != nil {
		f.bus.Subscribe(sub)
	}
}

func (f *chatterFacade) Shutdown(ctx context.Context) error {
	if f.bus != nil {
		return f.bus.Shutdown(ctx)
	}
	return nil
}

func (f *chatterFacade) emit(ctx context.Context, e events.Event) {
	if f.bus != nil {
		_ = f.bus.Publish(ctx, e)
	}
}
