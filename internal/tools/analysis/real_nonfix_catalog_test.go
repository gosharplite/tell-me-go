// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build arch

package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/require"
)

// TestVerifyNonFixCatalog is the partition gate for the Intentional Non-Fixes
// catalog: it recomputes the real complexity alerts against the LIVE repo and
// cross-references them against the live docs/architect/INTENTIONAL_NON_FIXES.md
// catalog (uncapped, mirroring bucketComplexityAlerts internals). Every
// over-threshold function must be pinned by an ACCEPTED catalog entry. The
// former known genuine gap (buildE2EBinary, CC=12) was refactored below
// threshold in 2026-09 (fix/e2e-build-complexity), so zero alerts remain.
//
// The go test binary runs with its working directory set to the package
// source directory, so the walk root and the catalog path are anchored to
// the module root via findModuleRoot() — the same precedent as
// TestVerifyRealArchitecture — to exercise the live repository and the live
// catalog regardless of invocation directory.
func TestVerifyNonFixCatalog(t *testing.T) {
	// Construct the real complexity analyzer exactly as production does
	// (manager.go newAnalysisManager): plain OS filesystem, mock security.
	analyzer := newComplexityAnalyzer(newASTCache("."), &mockSecurityProvider{}, persistencetest.NewPlainOSFileSystem())

	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	ctx := context.Background()
	complexities, _, err := analyzer.GatherComplexities(ctx, repoRoot, nil)
	require.NoError(t, err)

	// Load the REAL, live catalog (relative to the module root, since the
	// test binary's working directory is the package source directory).
	entries, err := loadNonFixCatalog(filepath.Join(repoRoot, defaultNonFixCatalogPath))
	require.NoError(t, err)

	// Partition over-threshold functions into cataloged vs actionable,
	// mirroring bucketComplexityAlerts internals but UNCAPPED (no 5-name
	// truncation): a function is cataloged when an ACCEPTED entry pins its
	// exact file:line; otherwise it is a genuine alert.
	cataloged := make(map[string]funcComplexity)
	var alerts []string
	for _, c := range complexities {
		if c.Complexity <= 10 {
			continue
		}
		normalized := normalizeCatalogFilePath(c.FilePath, repoRoot)
		if catalogTitleFor(entries, normalized, c.Line) != "" {
			cataloged[c.Name] = funcComplexity{Line: c.Line, Complexity: c.Complexity, FilePath: normalized}
			continue
		}
		alerts = append(alerts, c.Name)
	}

	expectedCataloged := map[string]funcComplexity{
		"TestHistoryManager_SetPinned_WithFunctionCall":             {Line: 392, Complexity: 21, FilePath: "internal/infrastructure/history/history_test.go"},
		"TestStartSpinnerLifecycle":                                 {Line: 176, Complexity: 17, FilePath: "internal/ui/renderer_spinner_test.go"},
		"TestGetModelTurn":                                          {Line: 1446, Complexity: 16, FilePath: "internal/infrastructure/history/history_test.go"},
		"TestPrepareMediaAssets_KimiURL_UploadsVideo":               {Line: 379, Complexity: 16, FilePath: "internal/infrastructure/llm/openai/files_test.go"},
		"TestHistoryNavigation_CompleteWorkflow":                    {Line: 161, Complexity: 15, FilePath: "tests/e2e/history_flags_test.go"},
		"(*indexer).snapshot":                                       {Line: 112, Complexity: 14, FilePath: "internal/tools/analysis/index_snapshot_test.go"},
		"(*model).Update":                                           {Line: 252, Complexity: 14, FilePath: "internal/ui/tui/progress/model.go"},
		"TestFixtureIndexer_ConstructAndHarvest":                    {Line: 158, Complexity: 13, FilePath: "internal/tools/analysis/index_fixture_test.go"},
		"TestHydrateMediaAssets":                                    {Line: 243, Complexity: 13, FilePath: "internal/infrastructure/llm/openai/client_vision_test.go"},
		"TestResolveCapabilities":                                   {Line: 10, Complexity: 13, FilePath: "internal/domain/llm/capabilities_test.go"},
		"assertMissingKeysResult":                                   {Line: 214, Complexity: 13, FilePath: "internal/infrastructure/llm/factory_test.go"},
		"(*RecoveryStep).Process":                                   {Line: 129, Complexity: 12, FilePath: "internal/agent/orchestrator/engine_phases.go"},
		"(*model).handleDomainEvent":                                {Line: 339, Complexity: 12, FilePath: "internal/ui/tui/progress/model.go"},
		"TestDeadCodeAnalyzer_Precision":                            {Line: 177, Complexity: 12, FilePath: "internal/tools/analysis/precision_test.go"},
		"TestHistoryManager_SetPinned_ViaModelID":                   {Line: 494, Complexity: 12, FilePath: "internal/infrastructure/history/history_test.go"},
		"TestRecoveryStep_EmptyResponse_RetriesUpToLimit":           {Line: 532, Complexity: 13, FilePath: "internal/agent/orchestrator/engine_phases_test.go"},
		"TestUpdateTurnContent_AddTextWhenNone":                     {Line: 1362, Complexity: 12, FilePath: "internal/infrastructure/history/history_test.go"},
		"TestUpdateTurnContent_ClearText":                           {Line: 1275, Complexity: 12, FilePath: "internal/infrastructure/history/history_test.go"},
		"TestVision_KimiImagePayload":                               {Line: 128, Complexity: 12, FilePath: "internal/infrastructure/llm/openai/client_vision_test.go"},
		"createPrecisionWorkspace":                                  {Line: 118, Complexity: 12, FilePath: "internal/tools/analysis/precision_test.go"},
		"renderHistory":                                             {Line: 26, Complexity: 12, FilePath: "internal/ui/history.go"},
		"(*renderer).makeSubscriber":                                {Line: 48, Complexity: 11, FilePath: "internal/ui/tui/progress/renderer.go"},
		"TestMediaBlocks":                                           {Line: 18, Complexity: 11, FilePath: "internal/infrastructure/llm/openai/client_vision_test.go"},
		"TestRunCommand_NonExitErrorWaitPath":                       {Line: 817, Complexity: 11, FilePath: "internal/tools/workspace/process_executor_stream_test.go"},
		"TestBasicAuthTransport":                                    {Line: 645, Complexity: 11, FilePath: "internal/infrastructure/mcp/client_test.go"},
		"TestFakeToolchainRunner_PresetValues":                      {Line: 36, Complexity: 28, FilePath: "internal/tools/toolstest/fake_toolchain_runner_test.go"},
		"TestFakeToolchainRunner_ZeroDefaults":                      {Line: 249, Complexity: 38, FilePath: "internal/tools/toolstest/fake_toolchain_runner_test.go"},
		"TestToSDKSchema_EmptyType_OmitsTypeKey":                    {Line: 20, Complexity: 19, FilePath: "internal/infrastructure/llm/gemini/tools_test.go"},
		"TestToOpenAISchema_EmptyTypeOmitsKey":                      {Line: 21, Complexity: 11, FilePath: "internal/infrastructure/llm/openai/tools_test.go"},
		"TestToOpenAITools_EmptyTypeNested_OmitsEmptyTypeKey":       {Line: 124, Complexity: 16, FilePath: "internal/infrastructure/llm/openai/tools_test.go"},
		"TestConvertSchema_NestedUnrepresentable_BecomesUntypedAny": {Line: 241, Complexity: 12, FilePath: "internal/tools/integrations/mcp/schema_test.go"},
		"(*MCPServerConfig).validate":                               {Line: 103, Complexity: 20, FilePath: "internal/domain/config/mcp_config.go"},
		"TestMCPFactory_ConcurrentBuild":                            {Line: 683, Complexity: 11, FilePath: "internal/infrastructure/di/mcp_factory_test.go"},
		"TestMCPFactory_ConcurrentBuild_FailingServerSkips":         {Line: 743, Complexity: 15, FilePath: "internal/infrastructure/di/mcp_factory_test.go"},
		"TestStdio_RoundTrip":                                       {Line: 112, Complexity: 11, FilePath: "internal/infrastructure/mcp/stdio_client_integration_test.go"},
		"TestStdio_LauncherTreePassThrough":                         {Line: 216, Complexity: 13, FilePath: "internal/infrastructure/mcp/stdio_client_integration_test.go"},
		"TestStdio_ChildDeathMidSession":                            {Line: 278, Complexity: 11, FilePath: "internal/infrastructure/mcp/stdio_client_integration_test.go"},
	}

	require.Equal(t, expectedCataloged, cataloged)
	require.Empty(t, alerts)
}

// TestVerifyCoveragePinsMatchLiveCatalog is the coverage-pin regression gate
// for the catalog matcher: it asserts the four formerly-HIGH coverage gaps
// (config.go:249-251, task_service.go:101-103, manager.go:574-576,
// manager.go:619-621) are matched by the LIVE catalog after the continuation
// ref parser fix and the 2026-09 re-anchors. A cataloged gap surfacing as
// actionable in the detailed coverage report is the exact regression this
// test prevents. Coverage pins have no name axis to verify against (ADR-054),
// so interval-overlap matching against the live catalog is the enforcement
// boundary.
func TestVerifyCoveragePinsMatchLiveCatalog(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	entries, err := loadNonFixCatalog(filepath.Join(repoRoot, defaultNonFixCatalogPath))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "live catalog must contain ACCEPTED entries")

	tests := []struct {
		name  string
		file  string
		start int
		end   int
	}{
		{"config.go call-site error branch", "internal/domain/config/config.go", 250, 252},
		{"task_service.go AppendTask body", "internal/domain/services/task_service.go", 101, 103},
		{"manager.go groupTurns error branch", "internal/agent/session/context/manager.go", 526, 528},
		{"manager.go capBestBlock error branch", "internal/agent/session/context/manager.go", 571, 573},
		{"failover.go ExtractDocument body", "internal/infrastructure/llm/failover.go", 131, 133},
		{"resilient_client.go ExtractDocument body", "internal/infrastructure/llm/resilient_client.go", 106, 108},
		{"mcp_factory.go resolveServerToken default case", "internal/infrastructure/di/mcp_factory.go", 223, 227},
		{"mcp_factory.go gh auth token resolver", "internal/infrastructure/di/mcp_factory.go", 67, 70},
		{"toolchain_factory.go registerSkillsShTools error", "internal/infrastructure/di/toolchain_factory.go", 124, 126},
		{"provider_health.go buildRequest error branch", "internal/infrastructure/llm/provider_health.go", 126, 128},
		{"provider_health.go getPingEndpoint panic", "internal/infrastructure/llm/provider_health.go", 238, 238},
		{"types.go NewID", "internal/domain/llm/types.go", 234, 236},
		{"repository.go Task accessors", "internal/domain/ports/repository.go", 112, 116},
		{"service.go EditLastTurn body", "internal/agent/service.go", 189, 191},
		{"repository.go GetID accessor", "internal/domain/ports/repository.go", 108, 108},
		{"agent.go configRefreshHook no-op stubs", "internal/agent/agent.go", 383, 384},
		{"client.go marshalInputSchema json.Marshal", "internal/infrastructure/mcp/client.go", 258, 260},
		{"client.go ListTools marshalInputSchema propagation", "internal/infrastructure/mcp/client.go", 120, 122},
		{"stdio_client.go ListTools marshalInputSchema propagation", "internal/infrastructure/mcp/stdio_client.go", 162, 165},
		{"stdio_client.go StdoutPipe error", "internal/infrastructure/mcp/stdio_client.go", 82, 85},
		{"stdio_client.go StdinPipe error", "internal/infrastructure/mcp/stdio_client.go", 87, 90},
		{"stdio_client.go resolveCommand generic error", "internal/infrastructure/mcp/stdio_client.go", 347, 347},
		{"stdio_client.go Close session.Close error", "internal/infrastructure/mcp/stdio_client.go", 240, 242},
		{"global_prompt_tracker.go prepareCompactedEntries", "internal/infrastructure/history/global_prompt_tracker.go", 420, 422},
		{"policy.go confirmAction call-site error", "internal/infrastructure/security/policy.go", 95, 98},
		{"manager.go RegisterSafePath Abs error", "internal/infrastructure/security/manager.go", 136, 139},
		{"manager.go RegisterReadOnlyPath Abs error", "internal/infrastructure/security/manager.go", 161, 164},
		{"state.go SetInfo marshal error", "internal/infrastructure/persistence/state.go", 65, 68},
		{"metrics.go appendSummaryToLog marshal", "internal/infrastructure/telemetry/metrics.go", 176, 178},
		{"toolchain_factory.go execRunner closure", "internal/infrastructure/di/toolchain_factory.go", 145, 147},
		{"sqlite_store.go Update RowsAffected", "internal/infrastructure/persistence/sqlite_store.go", 207, 209},
		{"sqlite_store.go Delete RowsAffected", "internal/infrastructure/persistence/sqlite_store.go", 224, 226},
		{"mcp_factory.go gh token resolver success", "internal/infrastructure/di/mcp_factory.go", 71, 71},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := catalogTitleForRange(entries, tt.file, tt.start, tt.end)
			require.NotEmpty(t, title, "range %s:%d-%d must be matched by an ACCEPTED catalog entry", tt.file, tt.start, tt.end)
		})
	}

	// The capBestBlock non-capped return pin (re-anchored 2026-09 from 582 to
	// 590 as the acceptance comment block grew; re-anchored 2026-08 (#1320)
	// from 590 to 542) must resolve to its catalog entry — previously it
	// surfaced as an uncataloged MEDIUM gap.
	t.Run("manager.go_capBestBlock_non_capped_return", func(t *testing.T) {
		title := catalogTitleForRange(entries, "internal/agent/session/context/manager.go", 542, 542)
		require.NotEmpty(t, title, "range manager.go:542 must be matched by an ACCEPTED catalog entry")
		require.Contains(t, title, "capBestBlock non-capped return")
	})
}

// TestDetailedCoverageReport_CatalogedGapsNotActionable is the end-to-end
// regression for the FULL detailed-coverage report path: real `go test
// -coverprofile` subprocess → profile parse → applyCatalogTitles against the
// LIVE catalog → report format. It pins the REPORT OUTPUT, not just the
// matcher: the formerly-HIGH config.go:249-251 gap must appear under
// [CATALOGED GAPS (ACCEPTED)] and never under [HIGH PRIORITY GAPS]. This test
// is RED on the pre-fix parser (the `:249-251` continuation ref is dropped,
// so config.go:249-251 surfaces as HIGH) and GREEN on the fixed code — it is
// the self-verification that future agents' get_detailed_coverage output
// agrees with the catalog.
func TestDetailedCoverageReport_CatalogedGapsNotActionable(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	// The report path shells out to the real `go` binary; run it from the
	// module root so the coverage profile records repo-relative paths (catalog
	// refs are repo-relative). The test binary's working directory is the
	// package source dir, so chdir here and restore on cleanup. All arch-tagged
	// tests are sequential, so the chdir cannot race a parallel test.
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Mirror production wiring (manager.go newAnalysisManager): the real `go`
	// executor feeds both the runner and Exec; the catalog is the default live
	// catalog; SP is the test mock (unused on this path).
	executor := &exec.RealExecutor{}
	m := &healthManager{
		SP:          &mockSecurityProvider{},
		Exec:        executor,
		Runner:      toolchain.NewGoRunner(executor),
		clk:         clock.RealClock{},
		catalogPath: defaultNonFixCatalogPath,
	}

	report, err := m.getDetailedCoverageReport(context.Background(), "./internal/domain/config", nil, nil)
	require.NoError(t, err)

	// The cataloged gap must never rank as actionable: with the fix, config's
	// only uncovered block (config.go:249-251) is ACCEPTED, so the report
	// emits no HIGH PRIORITY GAPS section at all.
	require.NotContains(t, report, "[HIGH PRIORITY GAPS]")
	// ... and the formerly-HIGH block is reported as an ACCEPTED cataloged gap.
	require.Contains(t, report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Contains(t, report, "config.go (Lines 250-252)")
	require.Contains(t, report, "validateProviderUniqueness")
}

// TestDetailedCoverageReport_ContextPackageCataloged is the end-to-end
// behavioral check for the re-anchored capBestBlock non-capped return pin:
// the formerly-MEDIUM manager.go:542 gap must appear under
// [CATALOGED GAPS (ACCEPTED)] with its ACCEPTED title, and must not surface
// as actionable in the High or Medium buckets. Same full report path as
// TestDetailedCoverageReport_CatalogedGapsNotActionable, scoped to the
// context package (~0.5s subprocess).
func TestDetailedCoverageReport_ContextPackageCataloged(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	// Run from the module root so the coverage profile records repo-relative
	// paths; restore on cleanup (all arch-tagged tests are sequential).
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Mirror production wiring (manager.go newAnalysisManager).
	executor := &exec.RealExecutor{}
	m := &healthManager{
		SP:          &mockSecurityProvider{},
		Exec:        executor,
		Runner:      toolchain.NewGoRunner(executor),
		clk:         clock.RealClock{},
		catalogPath: defaultNonFixCatalogPath,
	}

	report, err := m.getDetailedCoverageReport(context.Background(), "./internal/agent/session/context", nil, nil)
	require.NoError(t, err)

	// The re-anchored 542 gap must be cataloged, not actionable: it is listed
	// under [CATALOGED GAPS (ACCEPTED)] with the capBestBlock title ...
	require.Contains(t, report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Contains(t, report, "manager.go (Lines 542-542)")
	require.Contains(t, report, "capBestBlock non-capped return")
	// ... and the High/Medium buckets are empty.
	require.Contains(t, report, "- High Priority (Architectural): 0")
	require.Contains(t, report, "- Medium Priority (Technical Debt): 0")
}

// TestDetailedCoverageReport_AgentPackageCataloged is the end-to-end
// regression for the re-anchored EditLastTurn pin: the formerly-HIGH
// service.go:189-191 gap must appear under [CATALOGED GAPS (ACCEPTED)]
// with the delegation-wrapper title, and never under [HIGH PRIORITY GAPS].
func TestDetailedCoverageReport_AgentPackageCataloged(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	// The report path shells out to the real `go` binary; run it from the
	// module root so the coverage profile records repo-relative paths (catalog
	// refs are repo-relative). Restore on cleanup (all arch-tagged tests are
	// sequential, so the chdir cannot race a parallel test).
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Mirror production wiring (manager.go newAnalysisManager).
	executor := &exec.RealExecutor{}
	m := &healthManager{
		SP:          &mockSecurityProvider{},
		Exec:        executor,
		Runner:      toolchain.NewGoRunner(executor),
		clk:         clock.RealClock{},
		catalogPath: defaultNonFixCatalogPath,
	}

	report, err := m.getDetailedCoverageReport(context.Background(), "./internal/agent", nil, nil)
	require.NoError(t, err)

	// The re-anchored EditLastTurn gap must be cataloged, not actionable: it
	// must never surface in the High bucket ...
	require.NotContains(t, report, "[HIGH PRIORITY GAPS]")
	// ... and must be reported as an ACCEPTED cataloged gap with its
	// delegation-wrapper title.
	require.Contains(t, report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Contains(t, report, "service.go (Lines 189-191)")
	require.Contains(t, report, "EditLastTurn")
}

// TestDetailedCoverageReport_SkillsshPackageCataloged is the end-to-end
// regression for the interval-anchored skillssh catalog entry: the
// formerly-MEDIUM error-handling gaps (e.g. manager_impl.go:60-62) must
// appear under [CATALOGED GAPS (ACCEPTED)] and never as actionable. The
// report renders at most 10 cataloged gaps (maxItems), so the search.go
// fetchSkillMeta branches and the tools.go registration branches — later in
// profile order — are proven cataloged via the exact counts: 35 total gaps,
// 0 High / 0 Medium / 0 Low, 35 cataloged, with the 25 unreported ones
// folded into the truncation note.
func TestDetailedCoverageReport_SkillsshPackageCataloged(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	// Run from the module root so the coverage profile records repo-relative
	// paths; restore on cleanup (all arch-tagged tests are sequential).
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Mirror production wiring (manager.go newAnalysisManager).
	executor := &exec.RealExecutor{}
	m := &healthManager{
		SP:          &mockSecurityProvider{},
		Exec:        executor,
		Runner:      toolchain.NewGoRunner(executor),
		clk:         clock.RealClock{},
		catalogPath: defaultNonFixCatalogPath,
	}

	report, err := m.getDetailedCoverageReport(context.Background(), "./internal/tools/integrations/skillssh", nil, nil)
	require.NoError(t, err)

	// No actionable gaps of any priority: every uncovered block is ACCEPTED,
	// so no HIGH/MEDIUM section is rendered and the priority counts are 0.
	require.NotContains(t, report, "[HIGH PRIORITY GAPS]")
	require.Contains(t, report, "- High Priority (Architectural): 0")
	require.Contains(t, report, "- Medium Priority (Technical Debt): 0")
	require.Contains(t, report, "- Low Priority: 0")
	// The first rendered cataloged gaps (profile order, capped at 10) pin the
	// formerly-MEDIUM error branch in searchGitHubAPI ...
	require.Contains(t, report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Contains(t, report, "manager_impl.go (Lines 60-62)")
	// ... and the exact counts prove the remaining 25 cataloged blocks —
	// including all four search.go fetchSkillMeta branches (78-80, 83-85,
	// 88-90, 93-95) and the 15 tools.go registration branches, which sort
	// later in profile order and are folded into the truncation note — are
	// accepted, never actionable.
	require.Contains(t, report, "- Cataloged (ACCEPTED): 35")
	require.Contains(t, report, "... and 25 more cataloged (ACCEPTED) gaps.")
}

// TestDetailedCoverageReport_OpenAIPackageCataloged is the end-to-end
// regression for the issue #1387 catalog entries: after the 13 tests
// cover the OpenAI/Kimi file-upload adapter's error branches and the
// four multipart + one propagation branch are cataloged (Entry A/B),
// the openai package must report zero actionable gaps of any priority
// and exactly 5 cataloged (ACCEPTED) gaps. Same full report path as
// the sibling TestDetailedCoverageReport_* tests, scoped to the openai
// package.
func TestDetailedCoverageReport_OpenAIPackageCataloged(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	executor := &exec.RealExecutor{}
	m := &healthManager{
		SP:          &mockSecurityProvider{},
		Exec:        executor,
		Runner:      toolchain.NewGoRunner(executor),
		clk:         clock.RealClock{},
		catalogPath: defaultNonFixCatalogPath,
	}

	report, err := m.getDetailedCoverageReport(context.Background(), "./internal/infrastructure/llm/openai", nil, nil)
	require.NoError(t, err)

	require.NotContains(t, report, "[HIGH PRIORITY GAPS]")
	require.NotContains(t, report, "[MEDIUM PRIORITY GAPS]")
	require.Contains(t, report, "- High Priority (Architectural): 0")
	require.Contains(t, report, "- Medium Priority (Technical Debt): 0")
	require.Contains(t, report, "- Low Priority: 0")
	require.Contains(t, report, "- Cataloged (ACCEPTED): 5")
	require.Contains(t, report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Contains(t, report, "files.go (Lines 38-40)")
	require.Contains(t, report, "buildFileUploadBody multipart error branches")
}

// TestDetailedCoverageReport_ConfigPackageCataloged is the end-to-end
// regression for the cataloged normalizeKeyToken dead-branch entry: the
// structurally unreachable branches at redact.go:53-55 must appear under
// [CATALOGED GAPS (ACCEPTED)] with the dead-branch title, and never as an
// actionable gap. Same full report path as the sibling
// TestDetailedCoverageReport_* tests, scoped to the config package.
func TestDetailedCoverageReport_ConfigPackageCataloged(t *testing.T) {
	repoRoot, err := findModuleRoot()
	require.NoError(t, err)

	// Run from the module root so the coverage profile records repo-relative
	// paths; restore on cleanup (all arch-tagged tests are sequential).
	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Mirror production wiring (manager.go newAnalysisManager).
	executor := &exec.RealExecutor{}
	m := &healthManager{
		SP:          &mockSecurityProvider{},
		Exec:        executor,
		Runner:      toolchain.NewGoRunner(executor),
		clk:         clock.RealClock{},
		catalogPath: defaultNonFixCatalogPath,
	}

	report, err := m.getDetailedCoverageReport(context.Background(), "./internal/infrastructure/config", nil, nil)
	require.NoError(t, err)

	// The only uncovered block in the config package (redact.go:53-55) is
	// ACCEPTED, so it renders under [CATALOGED GAPS (ACCEPTED)] with the
	// dead-branch title ...
	require.Contains(t, report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Contains(t, report, "redact.go (Lines 53-55)")
	require.Contains(t, report, "normalizeKeyToken dead branches")
	// ... and the positional guard proves the interval is cataloged, not
	// actionable: actionable buckets render BEFORE the cataloged section, so
	// the gap must appear strictly after it.
	gapIdx := strings.Index(report, "redact.go (Lines 53-55)")
	catalogedIdx := strings.Index(report, "[CATALOGED GAPS (ACCEPTED)]")
	require.Greater(t, gapIdx, catalogedIdx, "redact.go (Lines 53-55) must appear under [CATALOGED GAPS (ACCEPTED)], not as an actionable gap")
}
