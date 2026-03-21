// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const maxTransientRetries = 3

// chatterFacade implements the ports.Chatter interface by coordinating specialized domain services.
type chatterFacade struct {
	mu sync.RWMutex

	contextPrep ContextPreparationService
	execution   ExecutionOrchestrator
	llmCoord    LLMCoordinator
	monitor     MonitoringTracker

	bus      events.EventBus
	registry tools.Registry
	history  ports.HistoryManager

	// Internal state/config
	maxToolTurns     int
	maxHistoryTokens int
	maxHistoryTurns  int
	tieredThreshold  int
	taskCost         float64
	providerName     string
	model            string
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
func WithRegistry(reg tools.Registry) facadeOption {
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

	transientRetries := 0

	startTime := time.Now()
	if s != nil && !s.StartTime.IsZero() {
		startTime = s.StartTime
	}

	// Main Turn Loop
	for turn := 0; turn <= f.getMaxToolTurns(); turn++ {
		stop, retry, err := f.executeTurn(ctx, turn, startTime)
		if err != nil {
			return err
		}
		if retry {
			transientRetries++
			if transientRetries > maxTransientRetries {
				return fmt.Errorf("exceeded max retries (%d) for transient errors", maxTransientRetries)
			}
			turn--
			continue
		}
		if stop {
			break
		}
	}

	return nil
}

func (f *chatterFacade) executeTurn(parentCtx context.Context, turn int, startTime time.Time) (stop bool, retry bool, err error) {
	// Guard check for context cancellation
	select {
	case <-parentCtx.Done():
		return false, false, parentCtx.Err()
	default:
	}

	trace := telemetry.NewTurnTrace()
	ctx := telemetry.ContextWithTrace(parentCtx, trace)

	// Signal turn start
	f.emit(ctx, events.TurnStarted{Turn: turn, MaxTurns: f.getMaxToolTurns()})

	// Prepare Context
	history, tokens, err := f.contextPrep.Prepare(ctx, turn)
	if err != nil {
		return f.handleError(ctx, trace, err, false)
	}

	f.emitTurnStatus(ctx, turn, tokens, nil, false, startTime)

	// Invoke LLM (Inference)
	response, metrics, isTransient, err := f.invokeLLM(ctx, history, trace)
	if err != nil {
		return f.handleError(ctx, trace, err, isTransient)
	}

	if err := f.contextPrep.AddContent(ctx, response); err != nil {
		return f.handleError(ctx, trace, fmt.Errorf("failed to persist response: %w", err), false)
	}

	// Tool Execution
	stop, retry, err = f.handleTools(ctx, response, turn, trace, metrics)

	// Metrics & Finalization
	f.finalizeTurn(ctx, turn, tokens, metrics, trace, err, retry, startTime)

	return stop, retry, err
}

func (f *chatterFacade) invokeLLM(ctx context.Context, history []*llm.Content, trace *telemetry.TurnTrace) (*llm.Content, *llm.Metrics, bool, error) {
	startInference := time.Now()
	response, metrics, err := f.llmCoord.Generate(ctx, history, f.registry.GetDeclarations(), f.history.GetResolver())
	trace.InferenceDuration = time.Since(startInference)

	if metrics != nil {
		f.mu.RLock()
		metrics.Provider = f.providerName
		metrics.Model = f.model
		f.mu.RUnlock()
	}

	if err != nil {
		return nil, nil, llm.IsTransient(err), err
	}
	return response, metrics, false, nil
}

func (f *chatterFacade) handleError(ctx context.Context, trace *telemetry.TurnTrace, err error, isTransient bool) (bool, bool, error) {
	if isTransient {
		f.emit(ctx, events.SystemMessageEvent{Message: fmt.Sprintf("Transient error: %v. Retrying...", err), Level: "warn"})
		trace.FinalStatus = "transient_error"
		f.emit(ctx, events.TraceEvent{Trace: trace})
		return false, true, nil
	}
	f.monitor.RecordError(ctx, err)
	trace.FinalStatus = "error"
	f.emit(ctx, events.TraceEvent{Trace: trace})
	return false, false, err
}

func (f *chatterFacade) handleTools(ctx context.Context, response *llm.Content, turn int, trace *telemetry.TurnTrace, metrics *llm.Metrics) (bool, bool, error) {
	if !f.hasToolCalls(response) {
		return true, false, nil
	}

	startTools := time.Now()
	stop, retry, err := f.runTools(ctx, response, turn)

	if metrics != nil {
		metrics.ToolDuration = time.Since(startTools).Seconds()
		metrics.CumulativeToolDuration = trace.CumulativeToolDuration().Seconds()
	}

	return stop, retry, err
}

func (f *chatterFacade) finalizeTurn(ctx context.Context, turn int, tokens int, metrics *llm.Metrics, trace *telemetry.TurnTrace, err error, retry bool, startTime time.Time) {
	// Track Usage AFTER tools
	turnCost, tErr := f.monitor.TrackUsage(ctx, metrics)
	if tErr != nil {
		f.monitor.RecordError(ctx, fmt.Errorf("telemetry failure: %w", tErr))
	}

	f.mu.Lock()
	f.taskCost += turnCost
	f.mu.Unlock()

	postTokens := tokens
	if metrics != nil && metrics.PromptTokens > 0 {
		postTokens = int(metrics.PromptTokens)
	}
	f.emitTurnStatus(ctx, turn, postTokens, metrics, true, startTime)

	trace.EndTime = time.Now()
	if err != nil {
		trace.FinalStatus = "error"
	} else if retry {
		trace.FinalStatus = "transient_error"
	} else {
		trace.FinalStatus = "success"
	}
	f.emit(ctx, events.TraceEvent{Trace: trace})
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
	toolResults, err := f.execution.Execute(ctx, response, turn, f.getMaxToolTurns())
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

func (f *chatterFacade) getMaxToolTurns() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.maxToolTurns
}

func (f *chatterFacade) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	f.mu.Lock()
	f.maxToolTurns = toolTurns
	f.maxHistoryTokens = historyTokens
	f.maxHistoryTurns = historyTurns
	limits := events.Limits{
		MaxToolTurns:     toolTurns,
		MaxHistoryTokens: historyTokens,
		MaxHistoryTurns:  historyTurns,
		TieredThreshold:  f.tieredThreshold,
	}
	f.mu.Unlock()
	f.emit(ctx, events.ConfigUpdated{Limits: limits})
	return nil
}

func (f *chatterFacade) SetTieredThreshold(ctx context.Context, threshold int) error {
	f.mu.Lock()
	f.tieredThreshold = threshold
	limits := events.Limits{
		MaxToolTurns:     f.maxToolTurns,
		MaxHistoryTokens: f.maxHistoryTokens,
		MaxHistoryTurns:  f.maxHistoryTurns,
		TieredThreshold:  threshold,
	}
	f.mu.Unlock()
	f.emit(ctx, events.ConfigUpdated{Limits: limits})
	return nil
}

func (f *chatterFacade) Subscribe(sub func(context.Context, events.Event)) {
	f.bus.Subscribe(sub)
}

func (f *chatterFacade) Shutdown(ctx context.Context) error {
	err := f.bus.Shutdown(ctx)
	if errors.Is(err, events.ErrBusNotInitialized) {
		return nil
	}
	return err
}

func (f *chatterFacade) emit(ctx context.Context, e events.Event) {
	_ = f.bus.Publish(ctx, e)
}

func (f *chatterFacade) emitTurnStatus(ctx context.Context, turn int, tokens int, metrics *llm.Metrics, isPostCall bool, startTime time.Time) {
	f.mu.RLock()
	maxHistTokens := f.maxHistoryTokens
	maxHistTurns := f.maxHistoryTurns
	threshold := f.tieredThreshold
	taskCost := f.taskCost
	f.mu.RUnlock()

	sessionTurns := 0
	if f.history != nil {
		sessionTurns = f.history.GetTotalEntries() / 2
	}

	var data StatusData
	if f.monitor != nil {
		data = f.monitor.GetStatusData(ctx)
	}

	f.emit(ctx, events.TurnStatusEvent{
		Status: events.TurnStatus{
			Timestamp:        time.Now(),
			StartTime:        startTime,
			CurrentTurns:     turn,
			SessionTurns:     sessionTurns,
			MaxHistoryTurns:  maxHistTurns,
			Tokens:           tokens,
			MaxHistoryTokens: maxHistTokens,
			TieredThreshold:  threshold,
			Metrics:          metrics,
			IsPostCall:       isPostCall,
			SessionCost:      data.Cost,
			DailyCost:        data.DailyCost,
			TaskCost:         taskCost,
			TotalM:           data.TotalModel,
			TotalH:           data.TotalHistory,
			TotalO:           data.TotalOutput,
		},
	})
}

// WithProvider sets the provider name for metrics injection.
func WithProvider(provider string) facadeOption {
	return func(f *chatterFacade) { f.providerName = provider }
}

// WithModel sets the model name for metrics injection.
func WithModel(model string) facadeOption {
	return func(f *chatterFacade) { f.model = model }
}
