package di

import (
	"context"
	"fmt"
	"net/http"
	"os"
	osexec "os/exec"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/agent"
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
	SkillRepo        domain_skills.SkillRepository
}

type defaultToolchainFactory struct {
	HomeDir          string
	FileSystem       infra_persistence.FileSystem
	SM               ConfigurableSecurityManager
	WorkspacePolicy  services.WorkspacePolicy
	RegisterAllTools func(params infra_tools.ToolRegistrationParams) error
	RegisterMetrics  func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing) error
}

func newToolchainFactory(homeDir string, fs infra_persistence.FileSystem, sm ConfigurableSecurityManager, wp services.WorkspacePolicy, registerAll func(params infra_tools.ToolRegistrationParams) error, registerMetrics func(r tools.Registry, sm security.Manager, logFile, traceFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing) error) toolchainFactory {
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

	// Single production construction of the runner (issue #1325: the
	// direct-construction class in tools is eliminated; only the di
	// composition root constructs). toolchainRunnerAdapter bridges the
	// infrastructure goRunner's CoverageReport return to the port's
	// CoverageSummary (TestOutput deliberately absent per ADR-060); it can be
	// replaced by a plain assignment once the infrastructure toolchain
	// satisfies tools.ToolchainRunner directly.
	runner := infra_toolchain.NewGoRunner(regParams.CommandExecutor)
	regParams.ToolchainRunner = toolchainRunnerAdapter{toolchainRunnerCore: runner}

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

// toolchainRunnerCore is the concrete method set of infra_toolchain.NewGoRunner:
// the 12 ToolchainRunner operations with the infrastructure CoverageReport
// return on RunTestsWithCoverage. It exists so toolchainRunnerAdapter can embed
// (and thus promote) every method except the one whose signature differs from
// the port. Satisfied by *infra_toolchain.goRunner.
type toolchainRunnerCore interface {
	GetPackageList(ctx context.Context, path string) ([]byte, error)
	GetGoDoc(ctx context.Context, symbol string) ([]byte, error)
	GetModulePath(ctx context.Context) (string, error)
	GetModuleDir(ctx context.Context) (string, error)
	RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (infra_toolchain.CoverageReport, error)
	RunLinter(ctx context.Context) (string, string, error)
	RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error)
	CheckGovulncheck(ctx context.Context) error
	RunModTidy(ctx context.Context) ([]byte, error)
	FormatCode(ctx context.Context, path string) ([]byte, error)
	RunTests(ctx context.Context, path string) ([]byte, error)
	BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error)
}

// toolchainRunnerAdapter bridges infra_toolchain's goRunner to the
// tools.ToolchainRunner port. The only signature difference is
// RunTestsWithCoverage: the infrastructure implementation returns its full
// CoverageReport while the port requires the tools-layer CoverageSummary
// (TestOutput intentionally absent — it has zero assertion consumers per
// ADR-060). All other methods are promoted from the embedded interface. Once
// the infrastructure toolchain returns tools.CoverageSummary directly
// (issue #1325 follow-up tasks), this adapter can be deleted and the plain
// assignment restored.
type toolchainRunnerAdapter struct {
	toolchainRunnerCore
}

func (a toolchainRunnerAdapter) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (tools.CoverageSummary, error) {
	report, err := a.toolchainRunnerCore.RunTestsWithCoverage(ctx, path, short, profilePath)
	return tools.CoverageSummary{
		PassedCount:   report.PassedCount,
		NoGoFiles:     report.NoGoFiles,
		CoveragePct:   report.CoveragePct,
		SummaryOutput: report.SummaryOutput,
	}, err
}
