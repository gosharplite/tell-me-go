package di

import (
	"fmt"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
)

type toolchainFactory interface {
	BuildRegistry(params toolchainParams) (tools.Registry, error)
}

type toolchainParams struct {
	Paths            *persistence.Paths
	SessionProvider  ports.SessionProvider
	HealthManager    ports.HealthCheckManager
	Client           llm.ExtendedClient
	Bus              events.EventBus
	Model            string
	Mode             string
	PricingOverrides map[string]pricing.ModelPricing
	Capturer         agent.CapturerInteractor
}

type defaultToolchainFactory struct {
	HomeDir          string
	FileSystem       infra_persistence.FileSystem
	SM               ConfigurableSecurityManager
	WorkspacePolicy  services.WorkspacePolicy
	RegisterAllTools func(params infra_tools.ToolRegistrationParams) error
	RegisterMetrics  func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error
}

func newToolchainFactory(homeDir string, fs infra_persistence.FileSystem, sm ConfigurableSecurityManager, wp services.WorkspacePolicy, registerAll func(params infra_tools.ToolRegistrationParams) error, registerMetrics func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error) toolchainFactory {
	return &defaultToolchainFactory{
		HomeDir:          homeDir,
		FileSystem:       fs,
		SM:               sm,
		WorkspacePolicy:  wp,
		RegisterAllTools: registerAll,
		RegisterMetrics:  registerMetrics,
	}
}

func (f *defaultToolchainFactory) BuildRegistry(params toolchainParams) (tools.Registry, error) {
	reg := registry.New()

	regParams := infra_tools.ToolRegistrationParams{
		Registry:         reg,
		SecurityManager:  f.SM,
		CommandExecutor:  &exec.RealExecutor{},
		CommandValidator: internal_security.NewCommandValidator(f.SM, params.Capturer),
		SessionProvider:  params.SessionProvider,
		LogFile:          params.Paths.LogPath,
		TraceFile:        params.Paths.TracePath,
		Model:            params.Model,
		Mode:             params.Mode,
		PricingOverrides: params.PricingOverrides,
		Client:           params.Client,
		AssetsDir:        filepath.Join(f.HomeDir, "assets", "generated"),
		EventBus:         params.Bus,
		FileSystem:       infra_persistence.NewDomainFS(f.FileSystem),
		HealthManager:    params.HealthManager,
		WorkspacePolicy:  f.WorkspacePolicy,
	}

	if err := f.RegisterAllTools(regParams); err != nil {
		return nil, fmt.Errorf("%w: failed to register core tools: %w", errInfraInit, err)
	}

	if err := f.RegisterMetrics(reg, f.SM, regParams.LogFile, regParams.TraceFile, regParams.Model, regParams.Mode, regParams.PricingOverrides, regParams.SessionProvider.GetSettings()); err != nil {
		return nil, fmt.Errorf("%w: failed to register metrics tools: %w", errInfraInit, err)
	}

	if err := f.SM.RegisterPolicyTools(reg, regParams.SessionProvider.GetSettings()); err != nil {
		return nil, fmt.Errorf("%w: failed to register policy tools: %w", errInfraInit, err)
	}

	return reg, nil
}
