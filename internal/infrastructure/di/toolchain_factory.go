package di

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	domain_skills "github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	infra_toolchain "github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/skillssh"
)

type toolchainFactory interface {
	BuildRegistry(params toolchainParams) (tools.Registry, error)
	BuildHealthChecker() ports.HealthChecker
	CloseMCPClients() error
	// GetMCPClient returns the stashed MCP client for a server key, or
	// (nil, false) when the server was skipped at construction.
	GetMCPClient(name string) (tools.MCPClient, bool)
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
	Capturer         ports.CapturerInteractor
	SkillRepo        domain_skills.SkillRepository
	MCPServers       map[string]config.MCPServerConfig
}

type defaultToolchainFactory struct {
	HomeDir          string
	FileSystem       infra_persistence.FileSystem
	SM               ConfigurableSecurityManager
	WorkspacePolicy  services.WorkspacePolicy
	Logger           *slog.Logger
	RegisterAllTools func(params infra_tools.ToolRegistrationParams) error
	RegisterMetrics  func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing) error

	// mcpFactory owns the lifecycle of every MCP client constructed by this
	// factory. It is held here (rather than created per BuildRegistry call)
	// so that session teardown can close all active MCP clients.
	mcpFactory *mcpFactory
}

func newToolchainFactory(homeDir string, fs infra_persistence.FileSystem, sm ConfigurableSecurityManager, wp services.WorkspacePolicy, logger *slog.Logger, registerAll func(params infra_tools.ToolRegistrationParams) error, registerMetrics func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing) error) toolchainFactory {
	return &defaultToolchainFactory{
		HomeDir:          homeDir,
		FileSystem:       fs,
		SM:               sm,
		WorkspacePolicy:  wp,
		Logger:           logger,
		RegisterAllTools: registerAll,
		RegisterMetrics:  registerMetrics,
		mcpFactory:       newMCPFactory(logger),
	}
}

func (f *defaultToolchainFactory) BuildRegistry(params toolchainParams) (tools.Registry, error) {
	reg := registry.New()

	mcpClients := f.mcpFactory.Build(params.MCPServers)

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
		MCPClients:       mcpClients,
	}

	// Single production construction of the runner (issue #1325, ADR-060):
	// the direct-construction class in tools is eliminated; only the di
	// composition root constructs. Reuses the &exec.RealExecutor{} from the
	// literal above; *toolchain.goRunner satisfies tools.ToolchainRunner
	// directly (CoverageSummary boundary).
	regParams.ToolchainRunner = infra_toolchain.NewGoRunner(regParams.CommandExecutor)

	if err := f.RegisterAllTools(regParams); err != nil {
		return nil, fmt.Errorf("%w: failed to register core tools: %w", errInfraInit, err)
	}

	if err := f.RegisterMetrics(reg, f.SM, regParams.LogFile, regParams.TraceFile, regParams.Model, regParams.Mode, regParams.PricingOverrides); err != nil {
		return nil, fmt.Errorf("%w: failed to register metrics tools: %w", errInfraInit, err)
	}

	if err := f.SM.RegisterPolicyTools(reg, regParams.SessionProvider.GetSettings()); err != nil {
		return nil, fmt.Errorf("%w: failed to register policy tools: %w", errInfraInit, err)
	}

	// Register skills.sh ecosystem tools
	if err := f.registerSkillsShTools(reg, params.SkillRepo); err != nil {
		return nil, fmt.Errorf("%w: failed to register skills.sh tools: %w", errInfraInit, err)
	}

	return reg, nil
}

// CloseMCPClients terminates all MCP clients constructed by this factory.
// It is wired into the session cleanup closure so active MCP sessions are
// closed when the chat session ends.
func (f *defaultToolchainFactory) CloseMCPClients() error {
	return f.mcpFactory.Close()
}

// GetMCPClient returns the stashed MCP client for a server key, or
// (nil, false) when the server was skipped at construction.
func (f *defaultToolchainFactory) GetMCPClient(name string) (tools.MCPClient, bool) {
	return f.mcpFactory.Client(name)
}

// registerSkillsShTools registers the four skills.sh ecosystem tools
// (search_skills, list_skills, install_skill, remove_skill) into the tool
// registry. It uses the pre-built skillRepo shared with the skill injector
// so that Refresh() calls from tools are visible to both consumers.
func (f *defaultToolchainFactory) registerSkillsShTools(r tools.Registry, skillRepo domain_skills.SkillRepository) error {
	skillsShDir := filepath.Join(f.HomeDir, ".skills")

	execRunner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return osexec.CommandContext(ctx, name, args...).CombinedOutput()
	}

	mgr := skillssh.NewSkillManager(skillsShDir, skillRepo, http.DefaultClient, execRunner, os.Getenv("GITHUB_TOKEN"))
	return skillssh.RegisterSkillsShTools(r, mgr)
}

// BuildHealthChecker creates a HealthChecker for the system toolchain binaries.
// The required and optional binary lists are owned here — they are toolchain
// implementation details, not DI orchestration concerns.
func (f *defaultToolchainFactory) BuildHealthChecker() ports.HealthChecker {
	return infra_toolchain.NewToolchainHealthChecker(
		&exec.RealExecutor{},
		[]string{"git", "go"}, // required binaries
		[]string{"make"},      // optional binaries
	)
}
