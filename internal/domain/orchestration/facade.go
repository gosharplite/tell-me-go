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
	maxToolTurns   int
	historyTokens  int
	historyTurns   int
	tieredThreshold int
}

// FacadeOption defines a functional option for initializing the chatterFacade.
type FacadeOption func(*chatterFacade)

func WithContextPrep(s ContextPreparationService) FacadeOption {
	return func(f *chatterFacade) { f.contextPrep = s }
}

func WithExecution(s ExecutionOrchestrator) FacadeOption {
	return func(f *chatterFacade) { f.execution = s }
}

func WithLLMCoord(s LLMCoordinator) FacadeOption {
	return func(f *chatterFacade) { f.llmCoord = s }
}

func WithMonitor(s MonitoringTracker) FacadeOption {
	return func(f *chatterFacade) { f.monitor = s }
}

func WithEventBus(bus events.EventBus) FacadeOption {
	return func(f *chatterFacade) { f.bus = bus }
}

func WithRegistry(reg tools.IToolRegistry) FacadeOption {
	return func(f *chatterFacade) { f.registry = reg }
}

func WithHistory(h ports.HistoryManager) FacadeOption {
	return func(f *chatterFacade) { f.history = h }
}

// NewChatterFacade creates a new Chatter implementation using the facade pattern.
func NewChatterFacade(opts ...FacadeOption) ports.Chatter {
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
	// 1. Initialize context with user prompt
	err := f.contextPrep.AddContent(ctx, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: prompt}},
	})
	if err != nil {
		return fmt.Errorf("failed to add user prompt: %w", err)
	}

	f.emit(ctx, events.StatusUpdate{Message: "Starting chat...", Level: "info"})

	// 2. Main Turn Loop
	for turn := 0; turn <= f.maxToolTurns; turn++ {
		// A. Guard check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// B. Signal turn start
		f.emit(ctx, events.TurnStarted{Turn: turn, MaxTurns: f.maxToolTurns})

		// C. Prepare Context
		history, err := f.contextPrep.Prepare(ctx, turn)
		if err != nil {
			f.monitor.RecordError(ctx, err)
			return err
		}

		// D. Invoke LLM (Inference)
		response, metrics, err := f.llmCoord.Generate(ctx, history, f.registry.GetDeclarations(), f.history.GetResolver())
		if err != nil {
			// Basic retry logic for transient errors
			if llm.IsTransient(err) {
				f.emit(ctx, events.SystemMessageEvent{Message: fmt.Sprintf("Transient error: %v. Retrying...", err), Level: "warn"})
				turn-- // Re-run this turn
				continue
			}
			f.monitor.RecordError(ctx, err)
			return err
		}

		// E. Track Usage
		if err := f.monitor.TrackUsage(ctx, metrics); err != nil {
			f.emit(ctx, events.SystemMessageEvent{Message: fmt.Sprintf("tracking failed: %v", err), Level: "warn"})
		}

		// F. Persist LLM Response
		if err := f.contextPrep.AddContent(ctx, response); err != nil {
			return fmt.Errorf("failed to persist response: %w", err)
		}

		// G. Check for Tool Calls
		hasToolCalls := false
		if response != nil {
			for _, p := range response.Parts {
				if p.FunctionCall != nil {
					hasToolCalls = true
					break
				}
			}
		}

		if !hasToolCalls {
			break // No tools to execute, turn complete
		}

		// H. Execute Tools
		toolResults, err := f.execution.Execute(ctx, response, turn, f.maxToolTurns)
		if err != nil {
			if llm.IsTransient(err) {
				f.emit(ctx, events.SystemMessageEvent{Message: fmt.Sprintf("Transient tool error: %v. Retrying...", err), Level: "warn"})
				// Note: In a robust engine we might want to checkpoint here, but for facade simplicity we'll just retry the turn
				continue
			}
			f.monitor.RecordError(ctx, err)
			return err
		}

		// I. Persist Tool Results
		if toolResults != nil {
			if err := f.contextPrep.AddContent(ctx, toolResults); err != nil {
				return fmt.Errorf("failed to persist tool results: %w", err)
			}
		}
	}

	return nil
}

func (f *chatterFacade) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.maxToolTurns = toolTurns
	f.historyTokens = historyTokens
	f.historyTurns = historyTurns
	return nil
}

func (f *chatterFacade) SetTieredThreshold(ctx context.Context, threshold int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tieredThreshold = threshold
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
