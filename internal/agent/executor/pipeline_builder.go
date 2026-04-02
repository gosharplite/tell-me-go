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
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
)

// A mocked ToolPipeline for tests that previously mocked Registry, SM, etc.
type MockToolPipeline struct {
	Registry                tools.Registry
	ExecuteFunc             func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult
	RequestBatchConsentFunc func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool)
	IsSerialFunc            func(toolName string) bool
}

func (m *MockToolPipeline) ExecuteTool(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, call)
	}
	return tools.ToolResult{Text: "mocked execute"}
}

func (m *MockToolPipeline) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	if m.RequestBatchConsentFunc != nil {
		return m.RequestBatchConsentFunc(ctx, calls)
	}
	return ctx, make(map[int]bool)
}

func (m *MockToolPipeline) IsSerial(toolName string) bool {
	if m.IsSerialFunc != nil {
		return m.IsSerialFunc(toolName)
	}
	if m.Registry != nil {
		return m.Registry.IsSerial(toolName)
	}
	return false
}

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

	// Create a pointer to Orchestrator to capture timeouts dynamically
	o := &Orchestrator{
		events:   bus,
		logger:   logger,
		observer: observer,
		zombie:   zombie,
	}

	o.state.Store(&orchestratorState{
		config: cfg,
		pool:   concurrency.NewWorkerPool(cfg.MaxConcurrentTools),
	})

	exec = newSafetyDecorator(exec, registry, logger, bus, zombie,
		func() time.Duration { return o.state.Load().config.ToolTimeout },
		func() time.Duration { return o.state.Load().config.LongRunningTimeout },
		func() time.Duration { return o.state.Load().config.ZombieTimeout },
	)

	basePipeline := &defaultToolPipeline{
		resolver:   newToolResolutionService(registry),
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}
	pipeline := NewCircuitBreakerPipeline(basePipeline, 3, 5*time.Minute)

	o.pipeline = pipeline
	// Run options which might update config or other things
	for _, opt := range opts {
		opt(o)
	}

	o.strategy = &markdownStrategy{}

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
