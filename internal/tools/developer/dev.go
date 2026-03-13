// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
)

type devManager struct {
	sm             devSecurity
	validator      domain_security.ICommandValidator
	executor       executor
	createTempFile func(dir, pattern string) (*os.File, error)
}

// Executor defines the interface for command execution to allow mocking in tests.
type executor interface {
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

func (m *devManager) runTests(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	parts, err := m.validateTestCommand(params.Command)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 3. User Authorization
	approved, err := m.authorizeAction(ctx, "Test Execution", params.Command, "Executing project tests")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Running Tests: %s", params.Command)

	// Execute the command directly without shell wrapper
	output, err := m.executor.Execute(ctx, parts[0], parts[1:]...)

	outStr := string(output)
	if err != nil {
		outStr = stringsutil.TruncateOutput(outStr, 100)
		// Return the failure output in the result, but still return an error for the status
		return tools.ToolResult{Text: fmt.Sprintf("FAIL:\n%s", outStr)}, fmt.Errorf("tests failed: %w", err)
	}

	return tools.ToolResult{Text: stringsutil.TruncateOutput(outStr, 100)}, nil
}

func (m *devManager) validateTestCommand(command string) ([]string, error) {
	if command == "" {
		return nil, fmt.Errorf("command argument is required")
	}

	// 1. Technical Validation: Split and check structure
	parts, err := m.validator.Split(command)
	if err != nil {
		return nil, fmt.Errorf("error parsing command: %w", err)
	}

	if err := m.validator.ValidateStructure(parts); err != nil {
		return nil, err
	}

	// 2. Path Safety: Ensure arguments don't escape allowed boundaries
	if safe, reason := m.validator.CheckPathSafety(parts); !safe {
		return nil, fmt.Errorf("security violation: %s", reason)
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
		return nil, fmt.Errorf("security violation: command '%s' is not an authorized test tool", baseCmd)
	}

	return parts, nil
}

func (m *devManager) authorizeAction(ctx context.Context, action, command, detail string) (bool, error) {
	isSafe, _ := m.validator.IsSafe(command)
	approved, err := m.sm.Authorize(ctx, action, command, detail, isSafe)
	if err != nil {
		return false, fmt.Errorf("authorization error: %w", err)
	}
	if !approved {
		return false, nil
	}

	// Use a consistent audit action name
	auditAction := strings.ToLower(strings.ReplaceAll(action, " ", "_"))
	m.sm.LogAudit("ACTION", auditAction, "COMMAND", command)
	return true, nil
}

func (m *devManager) goTidy(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	command := "go mod tidy && go fmt ./..."
	approved, err := m.authorizeAction(ctx, "Go Tidy", command, "Tidying project dependencies and formatting")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Running go mod tidy and go fmt")

	if out, err := m.executor.Execute(ctx, "go", "mod", "tidy"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("go mod tidy failed: %s", stringsutil.TruncateOutput(string(out), 50))
	}

	if out, err := m.executor.Execute(ctx, "go", "fmt", "./..."); err != nil {
		return tools.ToolResult{}, fmt.Errorf("go fmt failed: %s", stringsutil.TruncateOutput(string(out), 50))
	}

	return tools.ToolResult{Text: "Success: Project tidied and formatted."}, nil
}

func (m *devManager) getCoverage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "./..."
	}

	command := fmt.Sprintf("go test -coverprofile=coverage.out %s", path)
	approved, err := m.authorizeAction(ctx, "Test Coverage", command, "Getting test coverage summary")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Getting test coverage for %s", path)

	// Use a temporary file for coverage profile
	f, err := m.createTempFile("", "coverage-*.out")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempName := f.Name()
	f.Close()
	defer os.Remove(tempName)

	out, err := m.executor.Execute(ctx, "go", "test", "-coverprofile="+tempName, path)

	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("tests failed or coverage error: %w\n%s", err, stringsutil.TruncateOutput(string(out), 50))
	}

	// Get summary
	summaryOut, err := m.executor.Execute(ctx, "go", "tool", "cover", "-func="+tempName)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to generate coverage summary: %w", err)
	}

	return tools.ToolResult{Text: stringsutil.TruncateOutput(string(summaryOut), 100)}, nil
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
	approved, err := m.authorizeAction(ctx, "Linter Execution", fullCmd, "Running code analysis")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Running linter: %s", fullCmd)

	out, err := m.executor.Execute(ctx, command, argsList...)
	if err != nil && len(out) == 0 {
		return tools.ToolResult{}, fmt.Errorf("linter execution failed: %w", err)
	}

	outStr := stringsutil.TruncateOutput(string(out), 100)
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
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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
	approved, err := m.authorizeAction(ctx, "run_benchmark", command, "Running project benchmarks")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Running benchmarks (%s) in %s", bench, path)

	out, err := m.executor.Execute(ctx, "go", "test", "-bench="+bench, "-benchmem", "-run=^$", path)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("benchmark failed: %w\n%s", err, stringsutil.TruncateOutput(string(out), 100))
	}

	return tools.ToolResult{Text: string(out)}, nil
}

func (m *devManager) checkVulnerabilities(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if _, err := m.executor.LookPath("govulncheck"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest")
	}

	command := "govulncheck ./..."
	approved, err := m.authorizeAction(ctx, "Vulnerability Check", command, "Checking for known vulnerabilities")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Checking for vulnerabilities: %s", command)

	out, err := m.executor.Execute(ctx, "govulncheck", "./...")

	if err != nil && len(out) == 0 {
		return tools.ToolResult{}, fmt.Errorf("govulncheck failed: %w", err)
	}

	outStr := stringsutil.TruncateOutput(string(out), 100)
	if err != nil {
		return tools.ToolResult{Text: outStr}, fmt.Errorf("vulnerabilities found: %w", err)
	}

	if len(out) == 0 {
		return tools.ToolResult{Text: "No vulnerabilities found."}, nil
	}

	return tools.ToolResult{Text: outStr}, nil
}

func (m *devManager) logToolAction(format string, a ...any) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()
	m.sm.Warn(fmt.Sprintf("[Tool Action] "+format, a...))
}

func newDevManager(sm devSecurity, validator domain_security.ICommandValidator) *devManager {
	return &devManager{
		sm:             sm,
		validator:      validator,
		executor:       &realExecutor{},
		createTempFile: os.CreateTemp,
	}
}

type devSecurity interface {
	domain_security.ActionConfirmer
	domain_security.Auditor
	domain_security.TerminalController
}
