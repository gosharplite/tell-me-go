package di

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domainpersistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
)

// healthFactory defines the interface for building health check components.
type healthFactory interface {
	BuildHealthManager(cfg *config.Config, sessionProvider ports.SessionProvider, lazyClient llm.ExtendedClient, tf toolchainFactory) ports.HealthCheckManager
}

// defaultHealthFactory implements healthFactory.
type defaultHealthFactory struct {
	// fs is the domain filesystem port threaded into authenticator
	// construction (VertexAuth token-cache persistence).
	fs domainpersistence.FileSystem
}

// newHealthFactory creates a new defaultHealthFactory wired with the given
// domain filesystem.
func newHealthFactory(fs domainpersistence.FileSystem) healthFactory {
	return &defaultHealthFactory{fs: fs}
}

// BuildHealthManager creates a HealthCheckManager wired with the three standard
// health checkers: persistence, LLM provider, and toolchain.
func (f *defaultHealthFactory) BuildHealthManager(cfg *config.Config, sessionProvider ports.SessionProvider, lazyClient llm.ExtendedClient, tf toolchainFactory) ports.HealthCheckManager {
	p := cfg.GetActiveProvider()
	authenticator, _ := infra_llm.CreateAuthenticator(&p, f.fs) // Error is handled in health check itself

	healthCheckers := map[ports.Component]ports.HealthChecker{
		ports.CompPersistence: sessionProvider.GetHealthChecker(),
		ports.CompLLMProvider: infra_llm.NewLLMProviderHealthChecker(p.Family(), p.Type, authenticator, p.URL, lazyClient),
		ports.CompToolchain:   tf.BuildHealthChecker(),
	}

	return factory.NewHealthCheckManager(healthCheckers)
}
