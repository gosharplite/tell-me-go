package di

import (
	"log/slog"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// lazyRegistry wraps a registry factory and initializes the underlying
// registry on first call to get.
type lazyRegistry struct {
	once    sync.Once
	err     error
	reg     tools.Registry
	factory func() (tools.Registry, error)
	logger  ports.Logger
}

// newLazyRegistry creates a lazyRegistry backed by the given factory function.
func newLazyRegistry(factory func() (tools.Registry, error), logger ports.Logger) *lazyRegistry {
	return &lazyRegistry{factory: factory, logger: logger}
}

// get returns the initialized registry, or an error if initialization failed.
func (lr *lazyRegistry) get() (tools.Registry, error) {
	lr.once.Do(func() {
		lr.reg, lr.err = lr.factory()
		if lr.err != nil {
			lr.logger.Error("failed to lazily initialize tool registry", slog.Any("error", lr.err))
		}
	})
	return lr.reg, lr.err
}
