// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pricing"
)

// Clock provides a way to get the current time, facilitating deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// TurnPhase represents the current stage of a single agent turn.
type TurnPhase string

const (
	PhaseRefining   TurnPhase = "Refining"
	PhaseInference  TurnPhase = "Inference"
	PhaseExecuting  TurnPhase = "Executing"
	PhasePersisting TurnPhase = "Persisting"
	PhaseRecovering TurnPhase = "Recovering"
	PhaseComplete   TurnPhase = "Complete"
)

// ProcessResult describes the outcome of a phase execution.
type ProcessResult struct {
	NextPhase TurnPhase
	Error     error
	Stop      bool // Explicit signal to halt the turn
	Recovery  bool // Explicit signal that we should enter recovery
}

// RetryPolicy defines how the engine should handle errors and retries.
type RetryPolicy interface {
	ShouldRetry(err error, attempt int) (time.Duration, bool)
}

// DefaultRetryPolicy provides a standard retry implementation with linear backoff.
type DefaultRetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
}

func (p *DefaultRetryPolicy) ShouldRetry(err error, attempt int) (time.Duration, bool) {
	if attempt >= p.MaxRetries {
		return 0, false
	}
	if IsFatal(err) {
		return 0, false
	}
	if IsTransient(err) {
		return time.Duration(attempt+1) * p.Backoff, true
	}
	return 0, false
}

// TurnHook allows intercepting lifecycle events of a turn.
type TurnHook interface {
	BeforeTurn(turn *Turn)
	AfterTurn(turn *Turn, err error)
	OnPhaseTransition(from, to TurnPhase, state *TurnState)
}

// TurnState carries data between the phases of a turn and tracks the current phase.
type TurnState struct {
	Phase                TurnPhase        `json:"phase"`
	HasToolCalls         bool             `json:"has_tool_calls"`
	Metrics              *llm.Metrics     `json:"metrics,omitempty"`
	Tokens               int              `json:"tokens"`
	CurrentTurns         int              `json:"current_turns"`
	Metadata             *ContextMetadata `json:"metadata,omitempty"`
	Response             *llm.Content     `json:"response,omitempty"`
	ToolResponse         *llm.Content     `json:"tool_response,omitempty"`
	LastError            error            `json:"-"`
	RetryCount           int              `json:"retry_count"`
	ToolCallCount        map[string]int   `json:"-"`
	RecentResponseHashes []string         `json:"-"`
	PreparedHistory      []*llm.Content   `json:"-"`
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, turn *Turn) ProcessResult
}

// TurnProcessorFunc is an adapter to allow the use of ordinary functions as TurnProcessors.
type TurnProcessorFunc func(context.Context, *Turn) ProcessResult

// Process calls f(ctx, turn).
func (f TurnProcessorFunc) Process(ctx context.Context, turn *Turn) ProcessResult {
	return f(ctx, turn)
}

// TurnMiddleware wraps a TurnProcessor to inject cross-cutting concerns.
type TurnMiddleware func(TurnProcessor) TurnProcessor

// Turn carries state and configuration for a single agent turn.
type Turn struct {
	Index        int
	StartTime    time.Time
	State        *TurnState
	CtxManager   *ContextManager
	Gateway      llm.LLMGateway
	Executor     IToolExecutor
	Registry     tools.IToolRegistry
	Events       events.EventBus
	MaxToolTurns int
	Clock        Clock
	CostTracker  domain_pricing.ICostTracker
	Model        string

	// StreamHandler allows external handling of LLM response streams.
	StreamHandler func(context.Context, <-chan *llm.Content)

	// Results/Outputs
	Stop bool
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using a state machine.
type TurnEngine struct {
	mu               sync.RWMutex
	ctxManager       *ContextManager
	gateway          llm.LLMGateway
	executor         IToolExecutor
	registry         tools.IToolRegistry
	events           events.EventBus
	processors       map[TurnPhase]TurnProcessor
	middleware       []TurnMiddleware
	hooks            []TurnHook
	retryPolicy      RetryPolicy
	clock            Clock
	sm               security.ISecurityManager
	logFile          string
	model            string
	pricingOverrides map[string]pricing.ModelPricing
	costTracker      domain_pricing.ICostTracker
	HardBudgetLimit  float64 // Internal guardrail. Default 0.0 = Disabled.
	taskCost         float64 // Cumulative cost for the current Run() call.
}

// EngineOption allows configuring the TurnEngine.
type EngineOption func(*TurnEngine)

// WithMiddleware adds middleware to the TurnEngine.
func WithMiddleware(m ...TurnMiddleware) EngineOption {
	return func(e *TurnEngine) {
		e.middleware = append(e.middleware, m...)
	}
}

// WithProcessor registers or overrides a processor for a specific phase.
func WithProcessor(phase TurnPhase, p TurnProcessor) EngineOption {
	return func(e *TurnEngine) {
		e.processors[phase] = p
	}
}

// WithHook adds a lifecycle hook to the TurnEngine.
func WithHook(h TurnHook) EngineOption {
	return func(e *TurnEngine) {
		e.hooks = append(e.hooks, h)
	}
}

// WithRetryPolicy sets the retry policy for the TurnEngine.
func WithRetryPolicy(p RetryPolicy) EngineOption {
	return func(e *TurnEngine) {
		e.retryPolicy = p
	}
}

// WithClock sets the clock for the TurnEngine.
func WithClock(c Clock) EngineOption {
	return func(e *TurnEngine) {
		e.clock = c
	}
}

// WithHardBudget sets a maximum session budget in USD.
// Feature is intended for internal/API use only to maintain a clean UI.
func WithHardBudget(limit float64) EngineOption {
	return func(e *TurnEngine) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.HardBudgetLimit = limit
	}
}

// WithCostTracker sets the cost tracker for the engine.
func WithCostTracker(tracker domain_pricing.ICostTracker) EngineOption {
	return func(e *TurnEngine) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.costTracker = tracker
	}
}

// WithConfig sets the security and usage configuration for the engine.
func WithConfig(sm security.ISecurityManager, logFile, model string, pricingOverrides map[string]pricing.ModelPricing) EngineOption {
	return func(e *TurnEngine) {
		e.mu.Lock()
		defer e.mu.Unlock()

		e.sm = sm
		e.logFile = logFile
		e.model = model
		e.pricingOverrides = pricingOverrides
	}
}

// Reconfigure applies new options to the engine.
func (e *TurnEngine) Reconfigure(opts ...EngineOption) {
	for _, opt := range opts {
		opt(e)
	}
}

// NewTurnEngine creates a new TurnEngine with a default pipeline.
func NewTurnEngine(gw llm.LLMGateway, ex IToolExecutor, cm *ContextManager, reg tools.IToolRegistry, bus events.EventBus, opts ...EngineOption) *TurnEngine {
	e := &TurnEngine{
		gateway:     gw,
		executor:    ex,
		ctxManager:  cm,
		registry:    reg,
		events:      bus,
		processors:  make(map[TurnPhase]TurnProcessor),
		retryPolicy: &DefaultRetryPolicy{MaxRetries: 3, Backoff: 100 * time.Millisecond},
		clock:       realClock{},
	}

	// Register default processors
	e.processors[PhaseRefining] = &ContextRefiner{}
	e.processors[PhaseInference] = &InferenceStep{}
	e.processors[PhaseExecuting] = &ExecutionStep{}
	e.processors[PhasePersisting] = &PersistenceStep{}
	e.processors[PhaseRecovering] = &RecoveryStep{Policy: e.retryPolicy}

	for _, opt := range opts {
		opt(e)
	}

	// Ensure RecoveryStep uses the (potentially overridden) policy
	if rs, ok := e.processors[PhaseRecovering].(*RecoveryStep); ok {
		rs.Policy = e.retryPolicy
	}

	// Default middleware for eventing if bus is provided
	if e.events != nil {
		// Subscribe the cost tracker to metrics events via delegation to allow reconfiguration
		// without leaking subscribers or handling unsubscription.
		e.events.Subscribe(func(ev events.Event) {
			if um, ok := ev.(events.UsageMetricsEvent); ok {
				e.mu.RLock()
				tracker := e.costTracker
				e.mu.RUnlock()
				if tracker != nil && um.Metrics != nil {
					// Calculate cost for the UI and accumulate in one step
					um.Metrics.Cost = tracker.AccumulateAndReturn(*um.Metrics)
				}
			}
		})

		e.middleware = append(e.middleware,
			e.WithStreaming(),
			e.WithStatusReporter(),
			e.WithMetrics(),
			WithLoopDetector(),
		)
	}

	// Apply middleware in reverse order so the first one added is the outermost
	for i := len(e.middleware) - 1; i >= 0; i-- {
		m := e.middleware[i]
		for phase, p := range e.processors {
			e.processors[phase] = m(p)
		}
	}

	return e
}

// Run executes the multi-turn orchestration loop.
func (e *TurnEngine) Run(ctx context.Context, startTime time.Time) error {
	e.mu.Lock()
	e.taskCost = 0
	e.mu.Unlock()

	var lastState *TurnState
	sessionToolCallCount := make(map[string]int)
	for i := 0; ; i++ {
		if err := e.checkLimits(ctx, i); err != nil {
			return err
		}

		turn := e.createTurn(i, startTime)
		if lastState != nil {
			// Only carry over response hashes to detect text/turn repetition loops
			turn.State.RecentResponseHashes = append([]string(nil), lastState.RecentResponseHashes...)
		}
		// Tool calls are tracked at the session level to detect loops spanning multiple turn boundaries.
		turn.State.ToolCallCount = sessionToolCallCount

		e.notifyBeforeTurn(turn)

		err := e.executeTurn(ctx, turn)
		e.notifyAfterTurn(turn, err)

		if err != nil {
			return err
		}

		lastState = turn.State
		if e.shouldStopRunning(turn) {
			break
		}
	}
	return nil
}

func (e *TurnEngine) checkLimits(ctx context.Context, turnIndex int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Deterministic Budget Guardrail (API/Internal only)
	e.mu.RLock()
	limit := e.HardBudgetLimit
	tracker := e.costTracker
	e.mu.RUnlock()

	if limit > 0 && tracker != nil {
		if cost := tracker.GetTotalCost(ctx); cost >= limit {
			return NewAgentError(ErrFatal, fmt.Sprintf("current session cost $%.4f exceeds internal limit $%.4f", cost, limit), llm.ErrBudgetExceeded)
		}
	}

	_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
	if turnIndex > maxTurns {
		return NewAgentError(ErrFatal, fmt.Sprintf("turn %d exceeds limit %d", turnIndex, maxTurns), llm.ErrMaxTurnsReached)
	}

	if e.events != nil {
		e.events.Publish(events.TurnStarted{Turn: turnIndex, MaxTurns: maxTurns})
	}
	return nil
}

func (e *TurnEngine) createTurn(index int, startTime time.Time) *Turn {
	e.mu.RLock()
	tracker := e.costTracker
	model := e.model
	e.mu.RUnlock()

	turn := &Turn{
		Index:       index,
		StartTime:   startTime,
		State:       &TurnState{CurrentTurns: index, Phase: PhaseRefining, RetryCount: 0},
		CtxManager:  e.ctxManager,
		Gateway:     e.gateway,
		Executor:    e.executor,
		Registry:    e.registry,
		Events:      e.events,
		Clock:       e.clock,
		CostTracker: tracker,
		Model:       model,
	}
	_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()
	return turn
}

func (e *TurnEngine) notifyBeforeTurn(turn *Turn) {
	for _, h := range e.hooks {
		h.BeforeTurn(turn)
	}
}

func (e *TurnEngine) notifyAfterTurn(turn *Turn, err error) {
	for _, h := range e.hooks {
		h.AfterTurn(turn, err)
	}
}

func (e *TurnEngine) shouldStopRunning(turn *Turn) bool {
	return !turn.State.HasToolCalls || turn.Stop
}

func (e *TurnEngine) executeTurn(ctx context.Context, turn *Turn) error {
	for turn.State.Phase != PhaseComplete {
		res, err := e.executePhase(ctx, turn)
		if err != nil {
			// Emergency save: if we were interrupted (e.g. Ctrl+C) during inference or execution,
			// we might have partial content. Save it now using a background context
			// to ensure the write succeeds even though the main context is canceled.
			if turn.State.Response != nil && len(turn.State.Response.Parts) > 0 {
				if p, ok := e.processors[PhasePersisting]; ok {
					// Use a timeout for emergency persistence to prevent hanging the shutdown sequence.
					saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					_ = p.Process(saveCtx, turn)
				}
			}
			return err
		}
		if e.shouldBreak(turn, res) {
			if res.Error != nil {
				return res.Error
			}
			break
		}
	}
	return nil
}

func (e *TurnEngine) executePhase(ctx context.Context, turn *Turn) (ProcessResult, error) {
	processor, ok := e.processors[turn.State.Phase]
	if !ok {
		return ProcessResult{}, NewAgentError(ErrLogic, fmt.Sprintf("no processor for phase: %s", turn.State.Phase), nil)
	}

	res := processor.Process(ctx, turn)
	if res.Error != nil {
		turn.State.LastError = res.Error
	}

	next := e.determineNextPhase(turn.State.Phase, res)
	e.notifyTransition(turn.State.Phase, next, turn.State)
	turn.State.Phase = next

	if res.Error != nil && next == PhaseComplete {
		return res, res.Error
	}
	return res, nil
}

func (e *TurnEngine) shouldBreak(turn *Turn, res ProcessResult) bool {
	if res.Stop {
		turn.Stop = true
	}
	return turn.Stop && turn.State.Phase != PhaseComplete
}

func (e *TurnEngine) determineNextPhase(current TurnPhase, res ProcessResult) TurnPhase {
	if (res.Error != nil || res.Recovery) && current != PhaseRecovering {
		return PhaseRecovering
	}
	if res.NextPhase != "" {
		return res.NextPhase
	}
	return PhaseComplete
}

func (e *TurnEngine) notifyTransition(from, to TurnPhase, state *TurnState) {
	for _, h := range e.hooks {
		h.OnPhaseTransition(from, to, state)
	}
}

// ContextRefiner prepares the context for the LLM call.
type ContextRefiner struct{}

func (p *ContextRefiner) Process(ctx context.Context, turn *Turn) ProcessResult {
	history, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		category := ErrFatal
		if IsTransient(err) {
			category = ErrTransient
		}
		return ProcessResult{Error: NewAgentError(category, "context preparation failed", err)}
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount
	turn.State.PreparedHistory = history

	return ProcessResult{NextPhase: PhaseInference}
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	respContent, metrics, err := p.invokeModel(ctx, turn)
	if respContent != nil {
		p.updateState(turn, respContent, metrics)
	}

	if err != nil {
		category := ErrFatal
		if IsTransient(err) {
			category = ErrTransient
		}
		return ProcessResult{Error: NewAgentError(category, "inference failed", err)}
	}

	return p.routeBasedOnContent(respContent)
}

func (p *InferenceStep) invokeModel(ctx context.Context, turn *Turn) (*llm.Content, *llm.Metrics, error) {
	history := turn.State.PreparedHistory
	respCh, finalize := turn.Gateway.Generate(ctx, history, turn.Registry.GetDeclarations(), turn.CtxManager.History.GetResolver())

	if turn.StreamHandler != nil {
		turn.StreamHandler(ctx, respCh)
	} else {
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		// We return what we have (partial content) along with the error
		// so that the engine can attempt an emergency checkpoint.
		return respContent, metrics, err
	}
	if respContent == nil {
		return nil, nil, NewAgentError(ErrLogic, "api returned nil content", nil)
	}
	return respContent, metrics, nil
}

func (p *InferenceStep) updateState(turn *Turn, content *llm.Content, metrics *llm.Metrics) {
	turn.State.Response = content
	turn.State.Metrics = metrics
	if metrics != nil {
		metrics.Model = turn.Model
		turn.State.Tokens = int(metrics.PromptTokens)
	}
	turn.State.HasToolCalls = p.hasToolCalls(content)
}

func (p *InferenceStep) routeBasedOnContent(content *llm.Content) ProcessResult {
	if p.hasToolCalls(content) {
		return ProcessResult{NextPhase: PhaseExecuting}
	}
	return ProcessResult{NextPhase: PhasePersisting}
}

func (p *InferenceStep) hasToolCalls(content *llm.Content) bool {
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

// ExecutionStep executes tools if any.
type ExecutionStep struct{}

func (p *ExecutionStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	if !turn.State.HasToolCalls {
		return ProcessResult{NextPhase: PhasePersisting}
	}

	toolStart := turn.Clock.Now()

	toolResponse, err := turn.Executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)
	if err != nil {
		category := ErrFatal
		if IsTransient(err) {
			category = ErrTransient
		}
		return ProcessResult{Error: NewAgentError(category, "tool execution failed", err)}
	}

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = turn.Clock.Now().Sub(toolStart).Seconds()
	}
	return ProcessResult{NextPhase: PhasePersisting}
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	if turn.State.Response != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.Response); err != nil {
			category := ErrFatal
			if IsTransient(err) {
				category = ErrTransient
			}
			return ProcessResult{Error: NewAgentError(category, "history error", err)}
		}
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.ToolResponse); err != nil {
			category := ErrFatal
			if IsTransient(err) {
				category = ErrTransient
			}
			return ProcessResult{Error: NewAgentError(category, "failed to persist tool results", err)}
		}
	}

	return ProcessResult{NextPhase: PhaseComplete}
}

// RecoveryStep handles errors by deciding whether to retry or fail.
type RecoveryStep struct {
	Policy RetryPolicy
}

func (p *RecoveryStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	err := turn.State.LastError
	if err == nil {
		return ProcessResult{NextPhase: PhaseComplete}
	}

	delay, retry := p.Policy.ShouldRetry(err, turn.State.RetryCount)
	if !retry {
		return p.handleFailure(err)
	}

	return p.attemptRetry(ctx, turn, delay)
}

func (p *RecoveryStep) handleFailure(err error) ProcessResult {
	if IsTransient(err) {
		return ProcessResult{NextPhase: PhaseComplete, Error: fmt.Errorf("max retries reached: %w", err)}
	}
	return ProcessResult{NextPhase: PhaseComplete, Error: err}
}

func (p *RecoveryStep) attemptRetry(ctx context.Context, turn *Turn, delay time.Duration) ProcessResult {
	turn.State.RetryCount++

	if err := ctx.Err(); err != nil {
		return ProcessResult{Error: err}
	}

	select {
	case <-ctx.Done():
		return ProcessResult{Error: ctx.Err()}
	case <-time.After(delay):
	}

	return ProcessResult{NextPhase: PhaseInference}
}

// WithStreaming returns a middleware that injects a stream handler into the turn.
func (e *TurnEngine) WithStreaming() TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			if turn.State.Phase == PhaseInference && e.events != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *llm.Content) {
					e.events.Publish(events.ResponseStreamEvent{Context: ctx, Stream: stream})
				}
			}
			return next.Process(ctx, turn)
		})
	}
}

// WithStatusReporter returns a middleware that publishes turn status events.
func (e *TurnEngine) WithStatusReporter() TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			res := next.Process(ctx, turn)
			if e.events == nil || res.Error != nil {
				return res
			}

			if turn.State.Phase == PhaseRefining || turn.State.Phase == PhasePersisting {
				maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
				threshold := turn.CtxManager.Strategy.GetTieredThreshold()

				var cost float64
				var totalM, totalH, totalO int64
				if turn.CostTracker != nil {
					cost = turn.CostTracker.GetTotalCost(ctx)
					stats, _ := turn.CostTracker.GetStats(ctx)
					totalM = stats.PromptTokens - stats.CachedTokens
					totalH = stats.CachedTokens
					totalO = stats.ResponseTokens + stats.ThinkingTokens
				}

				e.mu.RLock()
				currentTaskCost := e.taskCost
				e.mu.RUnlock()

				e.events.Publish(events.TurnStatusEvent{
					Status: events.TurnStatus{
						Timestamp:        turn.Clock.Now(),
						CurrentTurns:     turn.Index,
						SessionTurns:     len(turn.CtxManager.History.GetContents()) / 2,
						MaxHistoryTurns:  maxHistTurns,
						Tokens:           turn.State.Tokens,
						MaxHistoryTokens: maxTokens,
						TieredThreshold:  threshold,
						Metrics:          turn.State.Metrics,
						IsPostCall:       turn.State.Phase == PhasePersisting,
						StartTime:        turn.StartTime,
						SessionCost:      cost,
						TaskCost:         currentTaskCost,
						TotalM:           totalM,
						TotalH:           totalH,
						TotalO:           totalO,
					},
				})
			}
			return res
		})
	}
}

// WithMetrics returns a middleware that publishes usage metrics.
func (e *TurnEngine) WithMetrics() TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			res := next.Process(ctx, turn)
			if e.events != nil && turn.State.Phase == PhasePersisting && turn.State.Metrics != nil {
				if turn.CostTracker != nil {
					e.mu.Lock()
					// Calculate the cost for this specific turn
					turnCost := turn.CostTracker.CalculateCost(*turn.State.Metrics)
					// Populate the field so the UI can display it
					turn.State.Metrics.Cost = turnCost
					// Accumulate into the task total
					e.taskCost += turnCost
					e.mu.Unlock()
				}

				e.events.Publish(events.UsageMetricsEvent{
					Context:   ctx,
					Metrics:   turn.State.Metrics,
					StartTime: turn.StartTime,
				})
			}
			return res
		})
	}
}

// WithLoopDetector returns a middleware that detects and breaks infinite tool loops.
func WithLoopDetector() TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			res := next.Process(ctx, turn)

			if turn.State.Phase == PhaseInference && res.Error == nil && turn.State.Response != nil {
				// 1. Multi-step loop detection (Text & Tool Calls)
				rawJSON, _ := json.Marshal(turn.State.Response)
				h := sha256.Sum256(rawJSON)
				currentHash := hex.EncodeToString(h[:])

				for _, prevHash := range turn.State.RecentResponseHashes {
					if currentHash == prevHash {
						return ProcessResult{
							Stop:  true,
							Error: NewAgentError(ErrLogic, "infinite loop detected: model is repeating a previous response (content or tool calls)", nil),
						}
					}
				}
				// Keep last N hashes (using the same repetition limit)
				turn.State.RecentResponseHashes = append(turn.State.RecentResponseHashes, currentHash)
				if len(turn.State.RecentResponseHashes) > config.DefaultMaxLoopRepetitions {
					turn.State.RecentResponseHashes = turn.State.RecentResponseHashes[1:]
				}

				// 2. Tool call loop detection (Immediate threshold)
				for _, p := range turn.State.Response.Parts {
					if p.FunctionCall != nil {
						args, _ := json.Marshal(p.FunctionCall.Args)
						key := p.FunctionCall.Name + ":" + string(args)
						turn.State.ToolCallCount[key]++
						if turn.State.ToolCallCount[key] > config.DefaultMaxLoopRepetitions {
							return ProcessResult{
								Stop:  true,
								Error: NewAgentError(ErrLogic, fmt.Sprintf("infinite loop detected: tool '%s' called with same arguments %d times", p.FunctionCall.Name, turn.State.ToolCallCount[key]), nil),
							}
						}
					}
				}
			}

			return res
		})
	}
}

// GetCostTracker returns the session cost tracker used by the engine.
func (e *TurnEngine) GetCostTracker() domain_pricing.ICostTracker {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.costTracker
}
