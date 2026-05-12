package di

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/factory"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	infra_toolchain "github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
)

// healthFactory defines the interface for building health check components.
type healthFactory interface {
	BuildHealthManager(cfg *config.Config, sessionProvider ports.SessionProvider, lazyClient llm.ExtendedClient) ports.HealthCheckManager
}

// defaultHealthFactory implements healthFactory.
type defaultHealthFactory struct{}

// newHealthFactory creates a new defaultHealthFactory.
func newHealthFactory() healthFactory {
	return &defaultHealthFactory{}
}

// BuildHealthManager creates a HealthCheckManager wired with the three standard
// health checkers: persistence, LLM provider, and toolchain.
func (f *defaultHealthFactory) BuildHealthManager(cfg *config.Config, sessionProvider ports.SessionProvider, lazyClient llm.ExtendedClient) ports.HealthCheckManager {
	p := cfg.GetActiveProvider()
	authenticator, _ := infra_llm.CreateAuthenticator(&p) // Error is handled in health check itself

	healthCheckers := map[ports.Component]ports.HealthChecker{
		ports.CompPersistence: sessionProvider.GetHealthChecker(),
		ports.CompLLMProvider: infra_llm.NewLLMProviderHealthChecker(p.Type, authenticator, p.URL, lazyClient),
		ports.CompToolchain:   infra_toolchain.NewToolchainHealthChecker(&exec.RealExecutor{}, []string{"git", "go"}, []string{"make"}),
	}

	return factory.NewHealthCheckManager(healthCheckers)
}
