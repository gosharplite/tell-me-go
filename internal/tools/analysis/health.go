// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
	"golang.org/x/sync/errgroup"
)

type healthManager struct {
	SP     security.PolicyEvaluator
	Exec   tools.CommandExecutor
	Runner AnalysisGoRunner
	Ana    *analysisManager
}

type healthResult struct {
	Status  string
	Details string
}

type healthSummary struct {
	Results map[string]healthResult
	Alerts  []string
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
	table := m.formatHealthTable(summary.Results, summary.Alerts)

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
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
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
		compStatus, compDetails, alerts = m.checkComplexity(gCtx, hb)
		return nil
	})

	// 5. Dead Code
	g.Go(func() error {
		deadStatus, deadDetails = m.checkDeadCode(gCtx, hb)
		return nil
	})

	if err := g.Wait(); err != nil {
		// Fallback: Return a degraded state summary if context fails or a future check fatals
		return healthSummary{
			Results: map[string]healthResult{
				"System Error": {Status: "FAIL", Details: fmt.Sprintf("Parallel checks interrupted: %v", err)},
			},
			Alerts: alerts, // Keep the existing alerts slice defined earlier in the function
		}
	}

	return healthSummary{
		Results: map[string]healthResult{
			"Tests":      {Status: testStatus, Details: testDetails},
			"Coverage":   {Status: coverageStatus, Details: coverageDetails},
			"Linting":    {Status: lintStatus, Details: lintDetails},
			"Complexity": {Status: compStatus, Details: compDetails},
			"Dead Code":  {Status: deadStatus, Details: deadDetails},
		},
		Alerts: alerts,
	}
}

func (m *healthManager) formatHealthTable(results map[string]healthResult, alerts []string) string {
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

	if report.CoveragePct != "" {
		if report.CoveragePct == "N/A" {
			cStatus = "N/A"
			cDetails = "Could not parse coverage"
		} else if report.NoGoFiles {
			cStatus = "N/A"
			cDetails = "No Go files found in target path"
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
		if errors.Is(err, toolchain.ErrNoSupportedLinter) {
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

func (m *healthManager) checkComplexity(ctx context.Context, hb chan<- struct{}) (string, string, []string) {
	// Complexity check is internal and doesn't need TerminalLock unless it uses a tool
	complexities, err := m.Ana.Complexity.GatherComplexities(ctx, ".", hb)
	if err != nil {
		return "ERROR", err.Error(), nil
	}

	threshold := 10
	var alerts []string
	highCount := 0
	for _, c := range complexities {
		if c.Complexity > threshold {
			highCount++
			if len(alerts) < 5 {
				alerts = append(alerts, fmt.Sprintf("`%s` (%d)", c.Name, c.Complexity))
			}
		}
	}

	if highCount == 0 {
		return "GOOD", "All functions under threshold", nil
	}

	return fmt.Sprintf("%d Alerts", highCount), fmt.Sprintf("%d functions > threshold (%d)", highCount, threshold), alerts
}

func (m *healthManager) checkDeadCode(ctx context.Context, hb chan<- struct{}) (string, string) {
	reports, err := m.Ana.DeadCode.GatherOrphanReports(ctx, ".", hb)
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
	if test == "FAIL" {
		recs = append(recs, "Fix failing tests immediately.")
	}
	if strings.HasSuffix(cov, "%") {
		var val float64
		if _, err := fmt.Sscanf(cov, "%f%%", &val); err == nil {
			if val < 80 {
				recs = append(recs, fmt.Sprintf("Coverage (%.1f%%) is below the 80%% target.", val))
			}
		}
	}
	if strings.Contains(comp, "Alerts") {
		recs = append(recs, "Refactor high-complexity functions.")
	}
	if strings.Contains(lint, "Issues") {
		recs = append(recs, "Address linting issues.")
	}
	if strings.Contains(dead, "Issues") {
		recs = append(recs, "Remove dead or effectively private code to improve encapsulation.")
	}

	if len(recs) == 0 {
		return "Project health is excellent."
	}

	return strings.Join(recs, " ")
}

func (m *healthManager) GetDetailedCoverage(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		path = "./..."
	}

	report, err := m.getDetailedCoverageReport(ctx, path, hb)
	if err != nil {
		return tools.ToolResult{Text: "Error: " + err.Error()}, nil
	}

	return tools.ToolResult{Text: report}, nil
}
