// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package dev

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
)

type devManager struct {
	sm        *security.SecurityManager
	validator *framework.CommandValidator
}

// Register adds developer-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
	}

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_tests",
		Description: "Executes project tests using authorized tools (go, pytest, npm, cargo, make). Returns 'PASS' or truncated failure logs. Shell metacharacters are forbidden for security.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"command": {
					Type:        "STRING",
					Description: "The test command to execute (e.g., 'go test ./...', 'npm test').",
				},
			},
			Required: []string{"command"},
		},
	}, m.runTests, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "go_tidy",
		Description: "Runs 'go mod tidy' and 'go fmt ./...'.",
	}, m.goTidy, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "get_coverage",
		Description: "Runs Go tests with coverage and returns the summary.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The package path to test (default './...')",
				},
			},
		},
	}, m.getCoverage, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_linter",
		Description: "Runs the first available linter (golangci-lint or staticcheck). Returns a list of findings or success message.",
	}, m.runLinter, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "run_benchmark",
		Description: "Runs Go benchmarks and returns performance metrics (ns/op, B/op).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The package path to benchmark (default './...')",
				},
				"bench": {
					Type:        "STRING",
					Description: "Regex for benchmarks to run (default '.')",
				},
			},
		},
	}, m.runBenchmark, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "check_vulnerabilities",
		Description: "Runs 'govulncheck'.",
	}, m.checkVulnerabilities, registry.ToolOptions{LongRunning: true})
}

func (m *devManager) runTests(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	command := params.Command
	if command == "" {
		return tools.ToolResult{}, fmt.Errorf("command argument is required")
	}

	// 1. Technical Validation: Split and check structure
	parts, err := m.validator.Split(command)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error parsing command: %v", err)}, nil
	}

	if err := m.validator.ValidateStructure(parts); err != nil {
		return tools.ToolResult{Text: err.Error()}, nil
	}

	baseCmd := parts[0]
	// Safety check: restricted to known test tools
	allowedTools := map[string]bool{
		"go":     true,
		"pytest": true,
		"npm":    true,
		"cargo":  true,
		"make":   true,
	}

	if !allowedTools[baseCmd] && !strings.HasSuffix(baseCmd, "run_tests.sh") {
		return tools.ToolResult{}, fmt.Errorf("security violation: command '%s' is not an authorized test tool", baseCmd)
	}

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running Tests: %s%s\n", colors.ColorCyan, command, colors.ColorReset)
	}()

	// Execute the command directly without shell wrapper
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()

	outStr := string(output)
	if err == nil {
		return tools.ToolResult{Text: "PASS"}, nil
	}

	// If failed, return truncated output to help diagnose
	lines := strings.Split(outStr, "\n")
	if len(lines) > 100 {
		outStr = strings.Join(lines[:100], "\n") + "\n... (Output truncated) ..."
	}

	return tools.ToolResult{Text: fmt.Sprintf("FAIL:\n%s", outStr)}, nil
}

func (m *devManager) goTidy(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running go mod tidy and go fmt%s\n", colors.ColorCyan, colors.ColorReset)
	}()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		return tools.ToolResult{}, fmt.Errorf("go mod tidy failed: %s", string(out))
	}

	fmtCmd := exec.CommandContext(ctx, "go", "fmt", "./...")
	if out, err := fmtCmd.CombinedOutput(); err != nil {
		return tools.ToolResult{}, fmt.Errorf("go fmt failed: %s", string(out))
	}

	return tools.ToolResult{Text: "Success: Project tidied and formatted."}, nil
}

func (m *devManager) getCoverage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "./..."
	}

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Getting test coverage for %s%s\n", colors.ColorCyan, path, colors.ColorReset)
	}()

	cmd := exec.CommandContext(ctx, "go", "test", "-coverprofile=coverage.out", path)
	out, err := cmd.CombinedOutput()

	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Tests failed or coverage error:\n%s", string(out))}, nil
	}

	// Get summary
	summaryCmd := exec.CommandContext(ctx, "go", "tool", "cover", "-func=coverage.out")
	summaryOut, err := summaryCmd.CombinedOutput()
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Failed to generate coverage summary: %v", err)}, nil
	}

	// Clean up
	os.Remove("coverage.out")

	lines := strings.Split(string(summaryOut), "\n")
	if len(lines) > 50 {
		return tools.ToolResult{Text: strings.Join(lines[:50], "\n") + "\n... (truncated)"}, nil
	}

	return tools.ToolResult{Text: string(summaryOut)}, nil
}

func (m *devManager) runLinter(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running linter%s\n", colors.ColorCyan, colors.ColorReset)
	}()

	// Try golangci-lint first, fallback to staticcheck
	var cmd *exec.Cmd
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		cmd = exec.CommandContext(ctx, "golangci-lint", "run")
	} else if _, err := exec.LookPath("staticcheck"); err == nil {
		cmd = exec.CommandContext(ctx, "staticcheck", "./...")
	} else {
		return tools.ToolResult{Text: "Error: No supported linter found (golangci-lint or staticcheck)."}, nil
	}

	out, _ := cmd.CombinedOutput()
	if len(out) == 0 {
		return tools.ToolResult{Text: "Linter passed successfully."}, nil
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) > 100 {
		return tools.ToolResult{Text: strings.Join(lines[:100], "\n") + "\n... (truncated)"}, nil
	}

	return tools.ToolResult{Text: string(out)}, nil
}

func (m *devManager) runBenchmark(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Bench string `json:"bench"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "./..."
	}
	bench := params.Bench
	if bench == "" {
		bench = "."
	}

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running benchmarks (%s) in %s%s\n", colors.ColorCyan, bench, path, colors.ColorReset)
	}()

	cmd := exec.CommandContext(ctx, "go", "test", "-bench="+bench, "-benchmem", "-run=^$", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Benchmark failed:\n%s", string(out))}, nil
	}

	return tools.ToolResult{Text: string(out)}, nil
}

func (m *devManager) checkVulnerabilities(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Checking for vulnerabilities with govulncheck%s\n", colors.ColorCyan, colors.ColorReset)
	}()

	if _, err := exec.LookPath("govulncheck"); err != nil {
		return tools.ToolResult{Text: "Error: 'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest"}, nil
	}

	cmd := exec.CommandContext(ctx, "govulncheck", "./...")
	out, _ := cmd.CombinedOutput()

	if len(out) == 0 {
		return tools.ToolResult{Text: "No vulnerabilities found."}, nil
	}

	return tools.ToolResult{Text: string(out)}, nil
}
