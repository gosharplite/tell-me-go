// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/sync/errgroup"
)

type healthManager struct {
	SP         security.PolicyEvaluator
	Exec       tools.CommandExecutor
	Runner     AnalysisGoRunner
	complexity complexityAnalyzer
	deadCode   deadCodeAnalyzer
	clk        clock.Clock

	// catalogPath is the location of the Intentional Non-Fixes catalog read
	// during health checks. Injected at construction (production wiring uses
	// the package default); tests inject a fixture path. The zero value
	// behaves like a missing catalog.
	catalogPath string

	// repoRoot caches the repository root resolved by resolveRepoRoot so the
	// go list subprocess runs at most once per healthManager lifetime.
	repoRoot     string
	repoRootOnce sync.Once
}

type healthResult struct {
	Status  string
	Details string
}

type healthSummary struct {
	Results map[string]healthResult
	Alerts  []string
	// CatalogedNote carries the Intentional Non-Fixes (ACCEPTED) exclusion
	// note for over-threshold complexity functions, separate from the
	// actionable alerts so the dashboard can render it under its own header.
	CatalogedNote string
}

func (m *healthManager) GetCodeHealth(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	select {
	case <-ctx.Done():
		return tools.ToolResult{Text: "Operation cancelled: " + ctx.Err().Error()}, nil
	default:
	}

	// Heartbeat while waiting for all parallel health checks
	done := make(chan struct{})
	defer close(done)
	go m.startHeartbeat(done, hb)

	summary := m.runParallelChecks(ctx, hb)
	table := m.formatHealthTable(summary.Results, summary.Alerts, summary.CatalogedNote)

	recommendation := m.generateRecommendation(
		summary.Results["Tests"].Status,
		summary.Results["Coverage"].Status,
		summary.Results["Linting"].Status,
		summary.Results["Complexity"].Status,
		summary.Results["Dead Code"].Status,
	)

	var sb strings.Builder
	sb.WriteString(table)
	if recommendation != "" {
		sb.WriteString("\n**Architectural Recommendation**: " + recommendation + "\n")
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (m *healthManager) startHeartbeat(done <-chan struct{}, hb chan<- struct{}) {
	clk := m.clk
	if clk == nil {
		clk = clock.RealClock{}
	}
	ticker := clk.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C():
			if hb != nil {
				select {
				case hb <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (m *healthManager) runParallelChecks(ctx context.Context, hb chan<- struct{}) healthSummary {
	var (
		testStatus, testDetails, coverageStatus, coverageDetails string
		lintStatus, lintDetails                                  string
		compStatus, compDetails                                  string
		deadStatus, deadDetails                                  string
		alerts                                                   []string
		catalogedNote                                            string
	)

	g, gCtx := errgroup.WithContext(ctx)

	// 1 & 2. Run Tests with Coverage
	g.Go(func() error {
		testStatus, testDetails, coverageStatus, coverageDetails = m.runTestsAndCoverage(gCtx)
		return nil
	})

	// 3. Linting
	g.Go(func() error {
		lintStatus, lintDetails = m.runLint(gCtx)
		return nil
	})

	// 4. Complexity
	g.Go(func() error {
		compStatus, compDetails, alerts, catalogedNote = m.checkComplexity(gCtx, hb)
		return nil
	})

	// 5. Dead Code
	g.Go(func() error {
		deadStatus, deadDetails = m.checkDeadCode(gCtx, hb)
		return nil
	})

	_ = g.Wait()

	return healthSummary{
		Results: map[string]healthResult{
			"Tests":      {Status: testStatus, Details: testDetails},
			"Coverage":   {Status: coverageStatus, Details: coverageDetails},
			"Linting":    {Status: lintStatus, Details: lintDetails},
			"Complexity": {Status: compStatus, Details: compDetails},
			"Dead Code":  {Status: deadStatus, Details: deadDetails},
		},
		Alerts:        alerts,
		CatalogedNote: catalogedNote,
	}
}

// formatHealthTable renders the dashboard table plus the complexity sections.
// alerts holds only actionable over-threshold bullets (the cataloged note is
// passed separately), so a non-empty alerts slice always means there is at
// least one real alert and the "Complexity Alerts" header is honest. When only
// the cataloged note exists (every over-threshold function ACCEPTED), the
// "Complexity Alerts" header must NOT appear; the note gets its own section.
func (m *healthManager) formatHealthTable(results map[string]healthResult, alerts []string, catalogedNote string) string {
	var sb strings.Builder
	sb.WriteString("### Project Health Dashboard\n")
	sb.WriteString("| Metric | Status | Details |\n")
	sb.WriteString("| :--- | :--- | :--- |\n")

	metrics := []string{"Tests", "Coverage", "Linting", "Complexity", "Dead Code"}
	for _, metric := range metrics {
		res := results[metric]
		_, _ = fmt.Fprintf(&sb, "| **%s** | %s | %s |\n", metric, res.Status, res.Details)
	}

	if len(alerts) > 0 {
		sb.WriteString("\n**Complexity Alerts (Threshold > 10):**\n")
		for _, alert := range alerts {
			_, _ = fmt.Fprintf(&sb, "- %s\n", alert)
		}
	}
	if catalogedNote != "" {
		sb.WriteString("\n**Cataloged (ACCEPTED) over threshold:**\n")
		_, _ = fmt.Fprintf(&sb, "- %s\n", catalogedNote)
	}
	return sb.String()
}

func (m *healthManager) runTestsAndCoverage(ctx context.Context) (tStatus, tDetails, cStatus, cDetails string) {
	report, err := m.Runner.RunTestsWithCoverage(ctx, "./...", true, "")

	if err == nil {
		tStatus = "PASS"
		tDetails = fmt.Sprintf("%d packages passed", report.PassedCount)
	} else {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			tStatus = "TIMEOUT"
			tDetails = "Tests timed out"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			tStatus = "CANCELLED"
			tDetails = "Tests cancelled"
		} else {
			tStatus = "FAIL"
			tDetails = "Some tests failed. Run `run_tests` for details."
		}
	}

	if report.NoGoFiles {
		cStatus = "N/A"
		cDetails = "No Go files found in target path"
	} else if report.CoveragePct != "" {
		if report.CoveragePct == "N/A" {
			cStatus = "N/A"
			cDetails = "Could not parse coverage"
		} else {
			cStatus = report.CoveragePct
			cDetails = "Target: > 80%"
		}
	} else {
		cStatus = "ERROR"
		cDetails = "Failed to generate coverage summary"
	}

	return
}

func (m *healthManager) runLint(ctx context.Context) (string, string) {
	outStr, tool, err := m.Runner.RunLinter(ctx)
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		if errors.Is(err, tools.ErrNoSupportedLinter) {
			return "SKIP", "No linter found"
		}
		return "ERROR", err.Error()
	}

	outStr = strings.TrimSpace(outStr)
	if outStr == "" {
		return "CLEAN", "All checks passed"
	}

	lines := strings.Split(outStr, "\n")
	count := 0
	issueRegex := regexp.MustCompile(`.*\.go:\d+:\d+:`)
	for _, line := range lines {
		if issueRegex.MatchString(line) {
			count++
		}
	}

	if count == 0 {
		return "CLEAN", "All checks passed"
	}
	return fmt.Sprintf("%d Issues", count), fmt.Sprintf("Using %s", tool)
}

// resolveRepoRoot returns the repository root used to normalize file paths
// against the Intentional Non-Fixes catalog. It prefers the module directory
// reported by the Go runner and falls back to the process working directory;
// an empty result means normalization can only rely on relative paths. The
// result is computed once per healthManager (the first call still runs the go
// list subprocess) and cached for subsequent calls; a nil Runner fallback
// (os.Getwd) is cached too, since the process working directory does not
// change mid-run.
func (m *healthManager) resolveRepoRoot(ctx context.Context) string {
	m.repoRootOnce.Do(func() {
		if m.Runner != nil {
			if dir, err := m.Runner.GetModuleDir(ctx); err == nil && dir != "" {
				m.repoRoot = dir
				return
			}
		}
		if wd, err := os.Getwd(); err == nil {
			m.repoRoot = wd
		}
	})
	return m.repoRoot
}

// loadNonFixEntries loads the Intentional Non-Fixes catalog, logging a
// warning on I/O failure and degrading gracefully to an empty catalog
// (all gaps treated as actionable).
func (m *healthManager) loadNonFixEntries() []nonFixEntry {
	entries, catErr := loadNonFixCatalog(m.catalogPath)
	if catErr != nil {
		slog.Warn("failed to load non-fix catalog; treating all gaps as actionable", "path", m.catalogPath, "error", catErr)
	}
	return entries
}

// bucketComplexityAlerts partitions over-threshold functions into actionable
// alerts and cataloged (ACCEPTED) names, preserving document order. It keeps
// the historical caps of 5 collected alert strings and 5 collected cataloged
// names; the returned counts are always exact.
func bucketComplexityAlerts(complexities []funcComplexity, entries []nonFixEntry, repoRoot string, threshold int) (alerts []string, catalogedNames []string, highCount, catalogedCount int) {
	for _, c := range complexities {
		if c.Complexity <= threshold {
			continue
		}
		// funcComplexity.FilePath comes from fs.Walk and may be absolute or
		// relative; normalize to the repo-relative "internal/..." form used
		// by the catalog. Unmatched (or unnormalizable) functions stay alerts.
		normalized := normalizeCatalogFilePath(c.FilePath, repoRoot)
		if catalogTitleFor(entries, normalized, c.Line) != "" {
			catalogedCount++
			if len(catalogedNames) < 5 {
				catalogedNames = append(catalogedNames, fmt.Sprintf("`%s` (%d)", c.Name, c.Complexity))
			}
			continue
		}
		highCount++
		if len(alerts) < 5 {
			alerts = append(alerts, fmt.Sprintf("`%s` (%d)", c.Name, c.Complexity))
		}
	}
	return
}

// checkComplexity reports complexity health as (status, details, alerts,
// catalogedNote). "GOOD" means nothing actionable: either every function is
// under the threshold, or every over-threshold function is cataloged (ACCEPTED)
// — the cataloged note is carried in details and returned separately so the
// dashboard can render it under its own header. alerts contains only
// actionable bullets; catalogedNote is the single exclusion-note bullet.
func (m *healthManager) checkComplexity(ctx context.Context, hb chan<- struct{}) (string, string, []string, string) {
	// Complexity check is internal and doesn't need TerminalLock unless it uses a tool
	complexities, _, err := m.complexity.GatherComplexities(ctx, ".", hb)
	if err != nil {
		return "ERROR", err.Error(), nil, ""
	}

	// Cross-reference over-threshold functions against the Intentional
	// Non-Fixes catalog: ACCEPTED complexity entries are reported separately
	// and do not count as actionable alerts. A load failure degrades
	// gracefully: every gap is treated as actionable.
	entries := m.loadNonFixEntries()
	repoRoot := m.resolveRepoRoot(ctx)

	threshold := 10
	alerts, catalogedNames, highCount, catalogedCount := bucketComplexityAlerts(complexities, entries, repoRoot, threshold)

	if highCount == 0 && catalogedCount == 0 {
		return "GOOD", "All functions under threshold", nil, ""
	}

	details := fmt.Sprintf("%d functions > threshold (%d)", highCount, threshold)
	catalogedNote := ""
	if catalogedCount > 0 {
		details += fmt.Sprintf("; %d cataloged (ACCEPTED) over threshold excluded", catalogedCount)
		catalogedNote = fmt.Sprintf("%d cataloged (ACCEPTED) functions over threshold excluded: %s",
			catalogedCount, strings.Join(catalogedNames, ", "))
	}

	if highCount == 0 {
		// Only cataloged (ACCEPTED) over-threshold functions: nothing
		// actionable. "GOOD" must not become "0 Alerts", which would trip
		// recommendComplexity's strings.Contains(comp, "Alerts") check.
		return "GOOD", details, alerts, catalogedNote
	}
	return fmt.Sprintf("%d Alerts", highCount), details, alerts, catalogedNote
}

func (m *healthManager) checkDeadCode(ctx context.Context, hb chan<- struct{}) (string, string) {
	reports, err := m.deadCode.GatherOrphanReports(ctx, ".", false, hb)
	if err != nil {
		return "ERROR", err.Error()
	}

	if len(reports) == 0 {
		return "CLEAN", "No orphaned symbols found"
	}

	deadCount := 0
	privateCount := 0
	for _, r := range reports {
		switch r.Severity {
		case "DEAD":
			deadCount++
		case "PRIVATE":
			privateCount++
		}
	}

	return fmt.Sprintf("%d Issues", len(reports)), fmt.Sprintf("%d DEAD, %d PRIVATE", deadCount, privateCount)
}

func (m *healthManager) generateRecommendation(test, cov, lint, comp, dead string) string {
	var recs []string
	if r := m.recommendTest(test); r != "" {
		recs = append(recs, r)
	}
	if r := m.recommendCoverage(cov); r != "" {
		recs = append(recs, r)
	}
	if r := m.recommendComplexity(comp); r != "" {
		recs = append(recs, r)
	}
	if r := m.recommendLint(lint); r != "" {
		recs = append(recs, r)
	}
	if r := m.recommendDeadCode(dead); r != "" {
		recs = append(recs, r)
	}

	if len(recs) == 0 {
		return "Project health is excellent."
	}

	return strings.Join(recs, " ")
}

func (m *healthManager) recommendTest(test string) string {
	if test == "FAIL" || test == "TIMEOUT" || test == "CANCELLED" || test == "ERROR" {
		return "Fix failing or timed-out tests immediately."
	}
	return ""
}

func (m *healthManager) recommendCoverage(cov string) string {
	if strings.HasSuffix(cov, "%") {
		var val float64
		if _, err := fmt.Sscanf(cov, "%f%%", &val); err == nil {
			if val < 80 {
				return fmt.Sprintf("Coverage (%.1f%%) is below the 80%% target.", val)
			}
		}
	} else if cov == "ERROR" || cov == "TIMEOUT" {
		return "Address issues preventing coverage analysis."
	}
	return ""
}

func (m *healthManager) recommendComplexity(comp string) string {
	if strings.Contains(comp, "Alerts") || comp == "ERROR" || comp == "TIMEOUT" {
		return "Refactor high-complexity functions."
	}
	return ""
}

func (m *healthManager) recommendLint(lint string) string {
	if strings.Contains(lint, "Issues") || lint == "ERROR" || lint == "TIMEOUT" {
		return "Address linting issues."
	}
	return ""
}

func (m *healthManager) recommendDeadCode(dead string) string {
	if strings.Contains(dead, "Issues") || dead == "ERROR" || dead == "TIMEOUT" {
		return "Remove dead or effectively private code to improve encapsulation."
	}
	return ""
}

// defaultCoverageExclusions mirrors the Makefile test-coverage grep -v list
// and the coverage-exclusions-explicit invariant
// (docs/domain-model/quality.modelith.yaml): the nine documented test-double
// directories. filterExcludedBlocks matches these as SUBSTRINGS of the
// block's File path, so each entry carries its trailing slash.
var defaultCoverageExclusions = []string{
	"internal/agent/agenttest/",
	"internal/agent/orchestrator/orchestratortest/",
	"internal/domain/config/configtest/",
	"internal/tools/analysis/analysistest/",
	"internal/cli/clitest/",
	"internal/domain/events/eventstest/",
	"internal/infrastructure/persistence/persistencetest/",
	"internal/tools/toolstest/",
	"internal/infrastructure/testing/",
}

func (m *healthManager) GetDetailedCoverage(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		path = "./..."
	}

	var excludedPackages []string
	if raw, ok := args["excluded_packages"]; ok {
		if arr, ok := raw.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					excludedPackages = append(excludedPackages, s)
				}
			}
		}
	} else {
		// Default to the nine documented test-double directories so an
		// unfiltered run matches the Makefile test-coverage gate and the
		// coverage-exclusions-explicit invariant (issue #1433). An explicitly
		// passed list — even an empty one — always wins; only an ABSENT
		// argument triggers the default.
		excludedPackages = defaultCoverageExclusions
	}

	report, err := m.getDetailedCoverageReport(ctx, path, excludedPackages, hb)
	if err != nil {
		return tools.ToolResult{Text: "Error: " + err.Error()}, nil
	}

	return tools.ToolResult{Text: report}, nil
}
