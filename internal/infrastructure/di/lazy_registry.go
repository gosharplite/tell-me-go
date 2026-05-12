package di

import (
	"log/slog"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// LazyRegistry wraps a registry factory and initializes the underlying
// registry on first call to Get.
type LazyRegistry struct {
	once    sync.Once
	err     error
	reg     tools.Registry
	factory func() (tools.Registry, error)
	logger  ports.Logger
}

// NewLazyRegistry creates a LazyRegistry backed by the given factory function.
func NewLazyRegistry(factory func() (tools.Registry, error), logger ports.Logger) *LazyRegistry {
	return &LazyRegistry{factory: factory, logger: logger}
}

// Get returns the initialized registry, or an error if initialization failed.
func (lr *LazyRegistry) Get() (tools.Registry, error) {
	lr.once.Do(func() {
		lr.reg, lr.err = lr.factory()
		if lr.err != nil {
			lr.logger.Error("failed to lazily initialize tool registry", slog.Any("error", lr.err))
		}
	})
	return lr.reg, lr.err
}
