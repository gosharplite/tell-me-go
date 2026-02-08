// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/analysis"
)

type HealthManager struct {
	SP  security.SecurityProvider
	Ana *analysis.AnalysisManager
}

func (m *HealthManager) GetCodeHealth(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	select {
	case <-ctx.Done():
		return tools.ToolResult{Text: "Operation cancelled: " + ctx.Err().Error()}, nil
	default:
	}

	// 1 & 2. Run Tests with Coverage (One execution for efficiency)
	testStatus, testDetails, coverageStatus, coverageDetails := m.runTestsAndCoverage(ctx)

	// 3. Linting
	lintStatus, lintDetails := m.runLint(ctx)

	// 4. Complexity
	compStatus, compDetails, alerts := m.checkComplexity(ctx)

	// 5. Security
	secStatus, secDetails := m.checkSecurity(ctx)

	// Format table
	var sb strings.Builder
	sb.WriteString("### Project Health Dashboard\n")
	sb.WriteString("| Metric | Status | Details |\n")
	sb.WriteString("| :--- | :--- | :--- |\n")
	sb.WriteString(fmt.Sprintf("| **Tests** | %s | %s |\n", testStatus, testDetails))
	sb.WriteString(fmt.Sprintf("| **Coverage** | %s | %s |\n", coverageStatus, coverageDetails))
	sb.WriteString(fmt.Sprintf("| **Linting** | %s | %s |\n", lintStatus, lintDetails))
	sb.WriteString(fmt.Sprintf("| **Complexity** | %s | %s |\n", compStatus, compDetails))
	sb.WriteString(fmt.Sprintf("| **Security** | %s | %s |\n", secStatus, secDetails))

	if len(alerts) > 0 {
		sb.WriteString("\n**Complexity Alerts (Threshold > 10):**\n")
		for _, alert := range alerts {
			sb.WriteString(fmt.Sprintf("- %s\n", alert))
		}
	}

	recommendation := m.generateRecommendation(testStatus, coverageStatus, lintStatus, compStatus, secStatus)
	if recommendation != "" {
		sb.WriteString("\n**Architectural Recommendation**: " + recommendation + "\n")
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (m *HealthManager) runTestsAndCoverage(ctx context.Context) (tStatus, tDetails, cStatus, cDetails string) {
	if os.Getenv("SKIP_HEALTH_EXECUTION") == "true" {
		return "PASS", "Skipped (test mode)", "80.0%", "Mocked"
	}

	m.SP.TerminalLock()
	defer m.SP.TerminalUnlock()

	f, err := os.CreateTemp("", "coverage-*.out")
	if err != nil {
		return "ERROR", "Failed to create temp coverage file", "ERROR", ""
	}
	tempPath := f.Name()
	f.Close()
	defer os.Remove(tempPath)

	cmd := exec.CommandContext(ctx, "go", "test", "-coverprofile="+tempPath, "./...")
	out, err := cmd.CombinedOutput()
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
	sumCmd := exec.CommandContext(ctx, "go", "tool", "cover", "-func="+tempPath)
	sumOut, err := sumCmd.CombinedOutput()
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

func (m *HealthManager) runLint(ctx context.Context) (string, string) {
	if os.Getenv("SKIP_HEALTH_EXECUTION") == "true" {
		return "CLEAN", "Skipped (test mode)"
	}

	m.SP.TerminalLock()
	defer m.SP.TerminalUnlock()

	var cmd *exec.Cmd
	var linter string
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		cmd = exec.CommandContext(ctx, "golangci-lint", "run")
		linter = "golangci-lint"
	} else if _, err := exec.LookPath("staticcheck"); err == nil {
		cmd = exec.CommandContext(ctx, "staticcheck", "./...")
		linter = "staticcheck"
	} else {
		return "SKIP", "No linter found"
	}

	out, _ := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return "CLEAN", "All checks passed"
	}

	lines := strings.Split(outStr, "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return fmt.Sprintf("%d Issues", count), fmt.Sprintf("Using %s", linter)
}

func (m *HealthManager) checkComplexity(ctx context.Context) (string, string, []string) {
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

func (m *HealthManager) checkSecurity(ctx context.Context) (string, string) {
	if os.Getenv("SKIP_HEALTH_EXECUTION") == "true" {
		return "CLEAN", "Skipped (test mode)"
	}

	m.SP.TerminalLock()
	defer m.SP.TerminalUnlock()

	if _, err := exec.LookPath("govulncheck"); err != nil {
		return "SKIP", "govulncheck not installed (run 'go install golang.org/x/vuln/cmd/govulncheck@latest')"
	}

	cmd := exec.CommandContext(ctx, "govulncheck", "./...")
	out, _ := cmd.CombinedOutput()
	outStr := string(out)

	if strings.Contains(outStr, "No vulnerabilities found") {
		return "CLEAN", "No known vulnerabilities"
	}

	return "VULNS", "Vulnerabilities detected."
}

func (m *HealthManager) generateRecommendation(test, cov, lint, comp, sec string) string {
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

	if len(recs) == 0 {
		return "Project health is excellent."
	}

	return strings.Join(recs, " ")
}

func (m *HealthManager) GetDetailedCoverage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok {
		path = "./..."
	}

	report, err := GetDetailedCoverageReport(path, ShellRunner)
	if err != nil {
		return tools.ToolResult{Text: "Error: " + err.Error()}, nil
	}

	return tools.ToolResult{Text: report}, nil
}
