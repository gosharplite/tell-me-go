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
	executor  Executor
}

// Executor defines the interface for command execution to allow mocking in tests.
type Executor interface {
	Execute(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPath(file string) (string, error)
}

type realExecutor struct{}

func (e *realExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (e *realExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// Register adds developer-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
		executor:  &realExecutor{},
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
		return tools.ToolResult{}, fmt.Errorf("error parsing command: %w", err)
	}

	if err := m.validator.ValidateStructure(parts); err != nil {
		return tools.ToolResult{}, err
	}

	// 2. Path Safety: Ensure arguments don't escape allowed boundaries
	if safe, reason := m.validator.CheckPathSafety(parts); !safe {
		return tools.ToolResult{}, fmt.Errorf("security violation: %s", reason)
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

	// Tighten script validation: only allow local run_tests.sh
	isAllowedScript := baseCmd == "./run_tests.sh" || baseCmd == "run_tests.sh"

	if !allowedTools[baseCmd] && !isAllowedScript {
		return tools.ToolResult{}, fmt.Errorf("security violation: command '%s' is not an authorized test tool", baseCmd)
	}

	// 3. User Authorization
	isSafe, _ := m.validator.IsSafe(command)
	approved, err := m.sm.Authorize(ctx, "Test Execution", command, "Executing project tests", isSafe)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Unauthorized by user"}, nil
	}

	m.sm.LogAudit("ACTION", "run_tests", "COMMAND", command)

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running Tests: %s%s\n", colors.ColorCyan, command, colors.ColorReset)
	}()

	// Execute the command directly without shell wrapper
	output, err := m.executor.Execute(ctx, parts[0], parts[1:]...)

	outStr := string(output)
	if err != nil {
		outStr = framework.TruncateOutput(outStr, 100)
		// Return the failure output in the result, but still return an error for the status
		return tools.ToolResult{Text: fmt.Sprintf("FAIL:\n%s", outStr)}, fmt.Errorf("tests failed: %w", err)
	}

	return tools.ToolResult{Text: framework.TruncateOutput(outStr, 100)}, nil
}

func (m *devManager) goTidy(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	command := "go mod tidy && go fmt ./..."
	isSafe, _ := m.validator.IsSafe(command)
	approved, err := m.sm.Authorize(ctx, "Go Tidy", command, "Tidying project dependencies and formatting", isSafe)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Unauthorized by user"}, nil
	}

	m.sm.LogAudit("ACTION", "go_tidy", "COMMAND", command)
	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running go mod tidy and go fmt%s\n", colors.ColorCyan, colors.ColorReset)
	}()

	if out, err := m.executor.Execute(ctx, "go", "mod", "tidy"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("go mod tidy failed: %s", framework.TruncateOutput(string(out), 50))
	}

	if out, err := m.executor.Execute(ctx, "go", "fmt", "./..."); err != nil {
		return tools.ToolResult{}, fmt.Errorf("go fmt failed: %s", framework.TruncateOutput(string(out), 50))
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

	command := fmt.Sprintf("go test -coverprofile=coverage.out %s", path)
	isSafe, _ := m.validator.IsSafe(command)
	approved, err := m.sm.Authorize(ctx, "Test Coverage", command, "Getting test coverage summary", isSafe)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Unauthorized by user"}, nil
	}

	m.sm.LogAudit("ACTION", "get_coverage", "PATH", path)

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Getting test coverage for %s%s\n", colors.ColorCyan, path, colors.ColorReset)
	}()

	out, err := m.executor.Execute(ctx, "go", "test", "-coverprofile=coverage.out", path)

	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("tests failed or coverage error: %w\n%s", err, framework.TruncateOutput(string(out), 50))
	}

	// Get summary
	summaryOut, err := m.executor.Execute(ctx, "go", "tool", "cover", "-func=coverage.out")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to generate coverage summary: %w", err)
	}

	// Clean up
	os.Remove("coverage.out")

	return tools.ToolResult{Text: framework.TruncateOutput(string(summaryOut), 100)}, nil
}

func (m *devManager) runLinter(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	// Try golangci-lint first, fallback to staticcheck
	var command string
	var argsList []string
	if _, lookErr := m.executor.LookPath("golangci-lint"); lookErr == nil {
		command = "golangci-lint"
		argsList = []string{"run"}
	} else if _, lookErr := m.executor.LookPath("staticcheck"); lookErr == nil {
		command = "staticcheck"
		argsList = []string{"./..."}
	} else {
		return tools.ToolResult{}, fmt.Errorf("no supported linter found (golangci-lint or staticcheck)")
	}

	fullCmd := command + " " + strings.Join(argsList, " ")
	isSafe, _ := m.validator.IsSafe(fullCmd)
	approved, err := m.sm.Authorize(ctx, "Linter Execution", fullCmd, "Running code analysis", isSafe)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Unauthorized by user"}, nil
	}

	m.sm.LogAudit("ACTION", "run_linter", "COMMAND", fullCmd)

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running linter: %s%s\n", colors.ColorCyan, fullCmd, colors.ColorReset)
	}()

	out, err := m.executor.Execute(ctx, command, argsList...)
	if err != nil && len(out) == 0 {
		return tools.ToolResult{}, fmt.Errorf("linter execution failed: %w", err)
	}

	outStr := framework.TruncateOutput(string(out), 100)
	if err != nil {
		return tools.ToolResult{Text: outStr}, fmt.Errorf("linter found issues: %w", err)
	}

	if len(out) == 0 {
		return tools.ToolResult{Text: "Linter passed successfully."}, nil
	}

	return tools.ToolResult{Text: outStr}, nil
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

	command := fmt.Sprintf("go test -bench=%s -benchmem -run=^$ %s", bench, path)
	isSafe, _ := m.validator.IsSafe(command)
	approved, err := m.sm.Authorize(ctx, "Benchmark Execution", command, "Running project benchmarks", isSafe)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Unauthorized by user"}, nil
	}

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Running benchmarks (%s) in %s%s\n", colors.ColorCyan, bench, path, colors.ColorReset)
	}()

	m.sm.LogAudit("ACTION", "run_benchmark", "COMMAND", command)

	out, err := m.executor.Execute(ctx, "go", "test", "-bench="+bench, "-benchmem", "-run=^$", path)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("benchmark failed: %w\n%s", err, framework.TruncateOutput(string(out), 100))
	}

	return tools.ToolResult{Text: string(out)}, nil
}

func (m *devManager) checkVulnerabilities(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if _, err := m.executor.LookPath("govulncheck"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest")
	}

	command := "govulncheck ./..."
	isSafe, _ := m.validator.IsSafe(command)
	approved, err := m.sm.Authorize(ctx, "Vulnerability Check", command, "Checking for known vulnerabilities", isSafe)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Unauthorized by user"}, nil
	}

	m.sm.LogAudit("ACTION", "check_vulnerabilities", "COMMAND", command)

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "%s[Tool Action] Checking for vulnerabilities: %s%s\n", colors.ColorCyan, command, colors.ColorReset)
	}()

	out, err := m.executor.Execute(ctx, "govulncheck", "./...")

	if err != nil && len(out) == 0 {
		return tools.ToolResult{}, fmt.Errorf("govulncheck failed: %w", err)
	}

	outStr := framework.TruncateOutput(string(out), 100)
	if err != nil {
		return tools.ToolResult{Text: outStr}, fmt.Errorf("vulnerabilities found: %w", err)
	}

	if len(out) == 0 {
		return tools.ToolResult{Text: "No vulnerabilities found."}, nil
	}

	return tools.ToolResult{Text: outStr}, nil
}
