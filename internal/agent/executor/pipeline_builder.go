package executor

import (
	"errors"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// NewPipelineOrchestrator is the primary production constructor for the execution pipeline.
// It assembles all necessary decorators (authorization, tracing, safety, circuit breaker)
// and returns a fully configured Orchestrator ready for domain use.
func NewPipelineOrchestrator(registry tools.Registry, sm domain_security.Manager, bus events.EventBus, logger ports.Logger, observer tools.ExecutionObserver, opts ...executorOption) (*Orchestrator, error) {
	cfg := OrchestratorConfig{
		MaxConcurrentTools: 5,
		ToolTimeout:        30 * time.Second,
		LongRunningTimeout: 5 * time.Minute,
		ZombieTimeout:      5 * time.Minute,
		CBThreshold:        3,
		CBResetTimeout:     5 * time.Minute,
	}

	for _, opt := range opts {
		opt(&cfg)
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

	exec = newSafetyDecorator(exec, registry, logger, bus, zombie, cfg.ToolTimeout, cfg.LongRunningTimeout, cfg.ZombieTimeout)

	basePipeline := &defaultToolPipeline{
		resolver:   newToolResolutionService(registry),
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}
	pipeline := NewCircuitBreakerPipeline(basePipeline, cfg.CBThreshold, cfg.CBResetTimeout)

	res, err := NewOrchestrator(cfg, pipeline, bus, logger, observer, opts...)
	if err != nil {
		return nil, err
	}

	res.zombie = zombie

	return res, nil
}


