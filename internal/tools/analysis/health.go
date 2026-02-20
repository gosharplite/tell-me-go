// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type healthManager struct {
	SP   security.PolicyEvaluator
	Exec tools.CommandExecutor
	Ana  *analysisManager
}

func (m *healthManager) GetCodeHealth(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	select {
	case <-ctx.Done():
		return tools.ToolResult{Text: "Operation cancelled: " + ctx.Err().Error()}, nil
	default:
	}

	var (
		testStatus, testDetails, coverageStatus, coverageDetails string
		lintStatus, lintDetails                                  string
		compStatus, compDetails                                  string
		alerts                                                   []string
		deadStatus, deadDetails                                  string
		secStatus, secDetails                                    string
	)

	var wg sync.WaitGroup
	wg.Add(5)

	// 1 & 2. Run Tests with Coverage
	go func() {
		defer wg.Done()
		testStatus, testDetails, coverageStatus, coverageDetails = m.runTestsAndCoverage(ctx)
	}()

	// 3. Linting
	go func() {
		defer wg.Done()
		lintStatus, lintDetails = m.runLint(ctx)
	}()

	// 4. Complexity
	go func() {
		defer wg.Done()
		compStatus, compDetails, alerts = m.checkComplexity(ctx)
	}()

	// 5. Dead Code
	go func() {
		defer wg.Done()
		deadStatus, deadDetails = m.runDeadCode(ctx)
	}()

	// 6. Security
	go func() {
		defer wg.Done()
		secStatus, secDetails = m.checkSecurity(ctx)
	}()

	wg.Wait()

	// Format table
	var sb strings.Builder
	sb.WriteString("### Project Health Dashboard\n")
	sb.WriteString("| Metric | Status | Details |\n")
	sb.WriteString("| :--- | :--- | :--- |\n")
	sb.WriteString(fmt.Sprintf("| **Tests** | %s | %s |\n", testStatus, testDetails))
	sb.WriteString(fmt.Sprintf("| **Coverage** | %s | %s |\n", coverageStatus, coverageDetails))
	sb.WriteString(fmt.Sprintf("| **Linting** | %s | %s |\n", lintStatus, lintDetails))
	sb.WriteString(fmt.Sprintf("| **Complexity** | %s | %s |\n", compStatus, compDetails))
	sb.WriteString(fmt.Sprintf("| **Dead Code** | %s | %s |\n", deadStatus, deadDetails))
	sb.WriteString(fmt.Sprintf("| **Security** | %s | %s |\n", secStatus, secDetails))

	if len(alerts) > 0 {
		sb.WriteString("\n**Complexity Alerts (Threshold > 10):**\n")
		for _, alert := range alerts {
			sb.WriteString(fmt.Sprintf("- %s\n", alert))
		}
	}

	recommendation := m.generateRecommendation(testStatus, coverageStatus, lintStatus, compStatus, secStatus, deadStatus)
	if recommendation != "" {
		sb.WriteString("\n**Architectural Recommendation**: " + recommendation + "\n")
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (m *healthManager) runTestsAndCoverage(ctx context.Context) (tStatus, tDetails, cStatus, cDetails string) {
	f, err := os.CreateTemp("", "health-*.out")
	if err != nil {
		return "ERROR", "Failed to create temp file", "N/A", err.Error()
	}
	tempPath := f.Name()
	f.Close()
	defer os.Remove(tempPath)

	out, err := m.Exec.CombinedOutput(ctx, "go", "test", "-coverprofile="+tempPath, "./...")
	outStr := string(out)

	if err == nil {
		tStatus = "PASS"
		// Count packages
		lines := strings.Split(strings.TrimSpace(outStr), "\n")
		passed := 0
		for _, line := range lines {
			if strings.HasPrefix(line, "ok") {
				passed++
			}
		}
		tDetails = fmt.Sprintf("%d packages passed", passed)
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

	// Coverage parsing
	sumOut, err := m.Exec.CombinedOutput(ctx, "go", "tool", "cover", "-func="+tempPath)
	if err != nil {
		cStatus = "ERROR"
		cDetails = "Failed to generate coverage summary"
		return
	}

	re := regexp.MustCompile(`total:\s+\(statements\)\s+(\d+\.\d+)%`)
	matches := re.FindStringSubmatch(string(sumOut))
	if len(matches) > 1 {
		cStatus = matches[1] + "%"
		cDetails = "Target: > 80%"
	} else {
		cStatus = "N/A"
		cDetails = "Could not parse coverage"
	}

	return
}

func (m *healthManager) runLint(ctx context.Context) (string, string) {
	var tool string
	var args []string
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		tool = "golangci-lint"
		args = []string{"run"}
	} else if _, err := exec.LookPath("staticcheck"); err == nil {
		tool = "staticcheck"
		args = []string{"./..."}
	} else {
		return "SKIP", "No linter found"
	}

	out, _ := m.Exec.CombinedOutput(ctx, tool, args...)
	outStr := strings.TrimSpace(string(out))
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

func (m *healthManager) checkComplexity(ctx context.Context) (string, string, []string) {
	// Complexity check is internal and doesn't need TerminalLock unless it uses a tool
	complexities, err := m.Ana.Complexity.GatherComplexities(ctx, ".")
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

func (m *healthManager) runDeadCode(ctx context.Context) (string, string) {
	findings, err := m.Ana.DeadCode.GatherOrphanReports(ctx, ".")
	if err != nil {
		return "ERROR", err.Error()
	}

	if len(findings) == 0 {
		return "CLEAN", "0 Items"
	}

	return "DEBT", fmt.Sprintf("%d Items", len(findings))
}

func (m *healthManager) checkSecurity(ctx context.Context) (string, string) {
	if _, err := exec.LookPath("govulncheck"); err != nil {
		return "SKIP", "govulncheck not installed (run 'go install golang.org/x/vuln/cmd/govulncheck@latest')"
	}

	out, _ := m.Exec.CombinedOutput(ctx, "govulncheck", "./...")
	outStr := string(out)

	if strings.Contains(outStr, "No vulnerabilities found") {
		return "CLEAN", "No known vulnerabilities"
	}

	return "VULNS", "Vulnerabilities detected."
}

func (m *healthManager) generateRecommendation(test, cov, lint, comp, sec, dead string) string {
	var recs []string
	if test == "FAIL" {
		recs = append(recs, "Fix failing tests immediately.")
	}
	if strings.Contains(sec, "VULNS") {
		recs = append(recs, "Review and fix security vulnerabilities.")
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
	if dead == "DEBT" {
		recs = append(recs, "Prune orphaned symbols to reduce technical debt.")
	}

	if len(recs) == 0 {
		return "Project health is excellent."
	}

	return strings.Join(recs, " ")
}

func (m *healthManager) getDetailedCoverage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		path = "./..."
	}

	report, err := getDetailedCoverageReport(ctx, path, m.Exec)
	if err != nil {
		return tools.ToolResult{Text: "Error: " + err.Error()}, nil
	}

	return tools.ToolResult{Text: report}, nil
}
