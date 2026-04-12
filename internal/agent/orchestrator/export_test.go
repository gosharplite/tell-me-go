// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// Exported types for external test package
type TurnPhase = turnPhase
type Turn = turn
type TurnState = turnState
type ProcessResult = processResult
type TurnProcessor = turnProcessor
type TurnProcessorFunc = turnProcessorFunc
type TurnMiddleware = turnMiddleware
type TurnHook = turnHook
type GuardStep = guardStep
type ContextRefiner = contextRefiner
type InferenceStep = inferenceStep
type ExecutionStep = executionStep
type PersistenceStep = persistenceStep
type RecoveryStep = recoveryStep
type DefaultRetryPolicy = defaultRetryPolicy
type RetryPolicy = retryPolicy

const (
	PhaseGuard      = phaseGuard
	PhaseRefining   = phaseRefining
	PhaseInference  = phaseInference
	PhaseExecuting  = phaseExecuting
	PhasePersisting = phasePersisting
	PhaseRecovering = phaseRecovering
	PhaseComplete   = phaseComplete
)

// Exported functions for external test package
func WithEngineClock(c clock.Clock) engineOption {
	return withEngineClock(c)
}

func WithEngineMiddleware(m ...turnMiddleware) engineOption {
	return withEngineMiddleware(m...)
}

func WithEngineProcessor(phase turnPhase, p turnProcessor) engineOption {
	return withEngineProcessor(phase, p)
}

func WithEngineHook(h turnHook) engineOption {
	return withEngineHook(h)
}

func WithEngineRetryPolicy(p retryPolicy) engineOption {
	return withEngineRetryPolicy(p)
}

func NewAgentError(category error, message string, err error) error {
	return newAgentError(category, message, err)
}

const LoopWarning = loopWarning

func (e *Engine) ExecuteTurn(ctx context.Context, tr *turn) error {
	return e.executeTurn(ctx, tr)
}

func (e *Engine) CreateTurn(index int, startTime time.Time) *turn {
	return e.createTurn(index, startTime)
}

func (e *Engine) Processors() map[turnPhase]turnProcessor {
	return e.processors
}

func (e *Engine) WithMetrics() turnMiddleware {
	return e.withMetrics()
}

func (e *Engine) WithStatusReporter() turnMiddleware {
	return e.withStatusReporter()
}

func (p *inferenceStep) InvokeModel(ctx context.Context, turn *turn) (*llm.Content, *llm.Metrics, error) {
	return p.invokeModel(ctx, turn)
}

func IsTransient(err error) bool {
	return isTransient(err)
}

func IsFatal(err error) bool {
	return isFatal(err)
}

func (e *Engine) EmergencySave(turn *turn) {
	e.emergencySave(turn)
}

func (e *Engine) PrepareNextTurn(turn *turn) {
	e.prepareNextTurn(turn)
}

func (p *executionStep) ValidatePayloadLimits(ctx context.Context, turn *turn) {
	p.validatePayloadLimits(ctx, turn)
}

func (p *inferenceStep) HasToolCalls(content *llm.Content) bool {
	return p.hasToolCalls(content)
}

func ValidatePayloadLimits(p *executionStep, ctx context.Context, turn *turn) {
	p.validatePayloadLimits(ctx, turn)
}

var ErrLogic = errLogic

func (e *Engine) ExecutePhase(ctx context.Context, turn *turn) (processResult, error) {
	return e.executePhase(ctx, turn)
}

func ExecutePhase(e *Engine, ctx context.Context, turn *turn) (processResult, error) {
	return e.executePhase(ctx, turn)
}

// These are already defined in turn_engine_helpers_test.go as exported names.
// We just need to make sure they are aliases if they were unexported there.
// But I exported them in helpers. So I will just alias them here if needed for naming consistency.

type MockExecutor = mockExecutor
type MockHistoryManager = mockHistoryManager
type MockClock = mockClock
type MockHook = mockHook
type MockRetryPolicy = mockRetryPolicy
type MockEngineCostTracker = mockEngineCostTracker
type TestTurnEnv = testTurnEnv
type CostCapturer = costCapturer
type MockTokenCounter = mockTokenCounter
type MockGateway = mockGateway
type MockToolRegistry = mockToolRegistry
type MockEventBusFail = mockEventBusFail
type MockSecurityManager = mockSecurityManager
type MockTransformer = mockTransformer

func NewTestContextManager(s *session.ContextStrategy, h ports.HistoryManager, bus events.EventBus) *session.ContextManager {
	return newTestContextManager(s, h, bus)
}

func SetupTurnEngineTest(t *testing.T) *testTurnEnv {
	return setupTurnEngineTest(t)
}

func NewCostCapturer(bus events.EventBus) *costCapturer {
	return newCostCapturer(bus)
}

func CreateProcessorForPhase(phase turnPhase) turnProcessor {
	return createProcessorForPhase(phase)
}

func SetupTransitionTurn(hasTools bool, phase turnPhase) *turn {
	return setupTransitionTurn(hasTools, phase)
}

func TruncateOversizedResponse(content *llm.Content, limit int, instruction string) {
	truncateOversizedResponse(content, limit, instruction)
}
