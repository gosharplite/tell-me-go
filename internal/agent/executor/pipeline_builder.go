package executor

import (
	"errors"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// NewPipelineDispatcher is the primary production constructor for the execution pipeline.
// It assembles all necessary decorators (authorization, tracing, safety, circuit breaker)
// and returns a fully configured Dispatcher ready for domain use.
func NewPipelineDispatcher(registry tools.Registry, sm domain_security.Manager, bus events.EventBus, logger ports.Logger, observer tools.ExecutionObserver, opts ...ExecutorOption) (*Dispatcher, error) {
	cfg := dispatcherConfig{
		MaxConcurrentTools: 5,
		ToolTimeout:        30 * time.Second,
		LongRunningTimeout: 5 * time.Minute,
		ZombieTimeout:      5 * time.Minute,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if registry == nil {
		return nil, errors.New("registry is required")
	}
	if bus == nil {
		return nil, errors.New("event bus is required")
	}
	if sm == nil {
		return nil, errors.New("security manager is required")
	}
	if observer == nil {
		return nil, errors.New("execution observer is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	zombie, _ := tools.NewZombieTool(observer)

	var exec ToolExecutor = newBaseRuntime(registry)

	resolver := newToolResolutionService(registry)
	authService := newSecurityAuthorizer(sm, registry, resolver)

	exec = newAuthDecorator(exec, authService)
	exec = newTracingDecorator(exec, registry, logger)

	exec = newSafetyDecorator(exec, registry, logger, bus, zombie, cfg.ToolTimeout, cfg.LongRunningTimeout, cfg.ZombieTimeout)

	basePipeline := &defaultToolPipeline{
		resolver:   resolver,
		authorizer: authService,
		runtime:    exec,
		registry:   registry,
	}

	res, _ := newDispatcher(cfg, basePipeline, bus, logger, observer, opts...)

	res.zombie = zombie

	return res, nil
}
