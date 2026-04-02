package executor

import (
	"context"
	"errors"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// A mocked ToolPipeline for tests that previously mocked Registry, SM, etc.

// Wrapper for tests to ease migration
func BuildOrchestrator(registry tools.Registry, sm domain_security.Manager, bus events.EventBus, logger ports.Logger, observer tools.ExecutionObserver, opts ...executorOption) (*Orchestrator, error) {
	cfg := OrchestratorConfig{
		MaxConcurrentTools: 5,
		ToolTimeout:        30 * time.Second,
		LongRunningTimeout: 5 * time.Minute,
		ZombieTimeout:      5 * time.Minute,
	}

	if registry == nil {
		return nil, errors.New("registry is required")
	}
	if observer == nil {
		return nil, errors.New("ExecutionObserver is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	zombie, err := tools.NewZombieTool(observer)
	if err != nil {
		return nil, err
	}

	var exec ToolExecutor = newBaseRuntime(registry)

	authService := newSecurityAuthorizer(sm, registry)

	exec = newAuthDecorator(exec, authService)
	// Circuit breaker is moved to orchestrator loop
	exec = newTracingDecorator(exec, registry, logger)

	var o *Orchestrator

	getToolTimeout := func() time.Duration {
		if o != nil && o.state.Load() != nil {
			return o.state.Load().config.ToolTimeout
		}
		return cfg.ToolTimeout
	}

	getLongRunningTimeout := func() time.Duration {
		if o != nil && o.state.Load() != nil {
			return o.state.Load().config.LongRunningTimeout
		}
		return cfg.LongRunningTimeout
	}

	getZombieTimeout := func() time.Duration {
		if o != nil && o.state.Load() != nil {
			return o.state.Load().config.ZombieTimeout
		}
		return cfg.ZombieTimeout
	}

	exec = newSafetyDecorator(exec, registry, logger, bus, zombie, getToolTimeout, getLongRunningTimeout, getZombieTimeout)

	basePipeline := &defaultToolPipeline{
		resolver:   newToolResolutionService(registry),
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}
	pipeline := NewCircuitBreakerPipeline(basePipeline, 3, 5*time.Minute)

	res, err := NewOrchestrator(cfg, pipeline, bus, logger, observer, opts...)
	if err != nil {
		return nil, err
	}

	res.zombie = zombie
	o = res

	return o, nil
}

type MockPipelineAuthorizer struct{}

func (m *MockPipelineAuthorizer) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	return ctx, make(map[int]bool)
}
func (m *MockPipelineAuthorizer) Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
	return nil
}
func (m *MockPipelineAuthorizer) IdentifyConsentItems(calls []*llm.FunctionCall) ([]int, map[int]bool) {
	return []int{}, make(map[int]bool)
}
