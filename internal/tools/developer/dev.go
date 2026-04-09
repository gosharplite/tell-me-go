// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package developer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
	"github.com/gosharplite/tell-me-go/internal/pkg/telemetry"
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
)

type devOption func(*devManager)

func withHeartbeatInterval(d time.Duration) devOption {
	return func(m *devManager) {
		m.heartbeatInterval = d
	}
}

type goRunner interface {
	RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error)
	RunLinter(ctx context.Context) (string, string, error)
}

type devManager struct {
	sm                devSecurity
	validator         domain_security.CommandValidator
	executor          executor
	runner            goRunner
	createTempFile    func(dir, pattern string) (*os.File, error)
	heartbeatInterval time.Duration
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

func (m *devManager) runTests(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	output, err := m.executeWithHeartbeat(
		ctx,
		"Test Execution",
		params.Command,
		"Executing project tests",
		parts[0],
		parts[1:],
		hb,
	)

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

	res := formatExecutionResult("Test execution", output, err, 100, "")
	if res.Error != nil {
		return tools.ToolResult{}, res.Error
	}
	return res, nil
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
		return nil, fmt.Errorf("%w: %s", domain_security.ErrSandboxViolation, reason)
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
		return nil, fmt.Errorf("%w: command '%s' is not an authorized test tool", domain_security.ErrSandboxViolation, baseCmd)
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
	m.sm.LogAudit(auditAction, "COMMAND", command)
	return true, nil
}

func (m *devManager) goTidy(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	command := "go mod tidy && go fmt ./..."
	approved, err := m.authorizeAction(ctx, "Go Tidy", command, "Tidying project dependencies and formatting")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Running go mod tidy and go fmt")

	defer telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)()

	if out, err := m.executor.Execute(ctx, "go", "mod", "tidy"); err != nil {
		res := formatExecutionResult("Go mod tidy", out, err, 50, "")
		if res.Error != nil {
			return tools.ToolResult{}, res.Error
		}
		return res, nil
	}

	if out, err := m.executor.Execute(ctx, "go", "fmt", "./..."); err != nil {
		res := formatExecutionResult("Go fmt", out, err, 50, "")
		if res.Error != nil {
			return tools.ToolResult{}, res.Error
		}
		return res, nil
	}

	return tools.ToolResult{Text: "Success: Project tidied and formatted."}, nil
}

func (m *devManager) getCoverage(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	// 1. Prevent Flag Injection
	if strings.HasPrefix(strings.TrimSpace(path), "-") {
		return tools.ToolResult{}, fmt.Errorf("%w: path argument cannot start with a hyphen", domain_security.ErrSandboxViolation)
	}

	// 2. Enforce Sandbox
	if safe, reason := m.validator.CheckPathSafety([]string{"go", path}); !safe {
		return tools.ToolResult{}, fmt.Errorf("%w: %s", domain_security.ErrSandboxViolation, reason)
	}

	// Prompt user for authorization
	command := fmt.Sprintf("Run coverage on %s", path)
	approved, err := m.authorizeAction(ctx, "Test Coverage", command, "Getting test coverage summary")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Getting test coverage for %s", path)

	defer telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)()

	report, err := m.runner.RunTestsWithCoverage(ctx, path, false, "")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("coverage test failed: %w", err)
	}

	if report.NoGoFiles {
		return tools.ToolResult{Text: "0.0% coverage (No Go files found in target path to test)"}, nil
	}

	res := tools.ToolResult{
		Text: stringsutil.TruncateOutput(report.SummaryOutput, 100),
	}
	return res, nil
}

func (m *devManager) runLinter(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	// 1. Authorization
	approved, err := m.authorizeAction(ctx, "Linter Execution", "go lint", "Running code analysis")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	// 2. Logging
	m.logToolAction("Running linter execution: go lint")

	// 3. Telemetry/Heartbeat
	defer telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)()

	// 4. Execution via runner
	out, tool, err := m.runner.RunLinter(ctx)

	// 5. Format and return
	res := formatExecutionResult("Linter ("+tool+")", []byte(out), err, 100, "Linter passed successfully.")
	if res.Error != nil {
		return tools.ToolResult{}, res.Error
	}
	return res, nil
}

func (m *devManager) runBenchmark(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	// 1. Prevent Flag Injection
	if strings.HasPrefix(strings.TrimSpace(path), "-") || strings.HasPrefix(strings.TrimSpace(bench), "-") {
		return tools.ToolResult{}, fmt.Errorf("%w: arguments cannot start with a hyphen", domain_security.ErrSandboxViolation)
	}

	// 2. Enforce Sandbox
	if safe, reason := m.validator.CheckPathSafety([]string{"go", path}); !safe {
		return tools.ToolResult{}, fmt.Errorf("%w: %s", domain_security.ErrSandboxViolation, reason)
	}

	fullCmd := fmt.Sprintf("Run benchmarks matching %s in %s", bench, path)
	approved, err := m.authorizeAction(ctx, "run_benchmark", fullCmd, "Running project benchmarks")
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, tools.ErrUserDeclined
	}

	m.logToolAction("Running run_benchmark: %s", fullCmd)

	defer telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)()

	outStr, err := m.runner.RunBenchmarks(ctx, path, bench)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("benchmark failed or found issues: %w", err)
	}

	if strings.Contains(outStr, "No Go files found in target path to benchmark") {
		return tools.ToolResult{Text: outStr}, nil
	}

	res := formatExecutionResult("Benchmark", []byte(outStr), nil, 100, "Benchmark completed.")
	if res.Error != nil {
		return tools.ToolResult{}, res.Error
	}
	return res, nil
}

func (m *devManager) checkVulnerabilities(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if _, err := m.executor.LookPath("govulncheck"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("'govulncheck' is not installed. Please install it with: go install golang.org/x/vuln/cmd/govulncheck@latest")
	}

	command := "govulncheck"
	argsList := []string{"./..."}
	fullCmd := command + " " + strings.Join(argsList, " ")

	out, err := m.executeWithHeartbeat(
		ctx,
		"Vulnerability Check",
		fullCmd,
		"Checking for known vulnerabilities",
		command,
		argsList,
		hb,
	)

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

	res := formatExecutionResult("Govulncheck", out, err, 100, "No vulnerabilities found.")
	if res.Error != nil {
		return tools.ToolResult{}, res.Error
	}
	return res, nil
}

func (m *devManager) logToolAction(format string, a ...any) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()
	m.sm.Warn(fmt.Sprintf("[Tool Action] "+format, a...))
}

func formatExecutionResult(displayName string, out []byte, execErr error, truncateLimit int, emptySuccessMsg string) tools.ToolResult {
	if execErr != nil && len(out) == 0 {
		return tools.ToolResult{Error: fmt.Errorf("%s execution failed: %w", displayName, execErr)}
	}

	outStr := string(out)
	if truncateLimit > 0 {
		outStr = stringsutil.TruncateOutput(outStr, truncateLimit)
	}

	if execErr != nil {
		return tools.ToolResult{
			Text: fmt.Sprintf("%s failed or found issues:\n%s\nError: %v", displayName, outStr, execErr),
		}
	}

	if len(out) == 0 && emptySuccessMsg != "" {
		return tools.ToolResult{Text: emptySuccessMsg}
	}

	return tools.ToolResult{Text: outStr}
}

func (m *devManager) executeWithHeartbeat(
	ctx context.Context,
	actionName, fullCmd, reason string,
	command string, args []string,
	hb chan<- struct{},
) ([]byte, error) {
	// 1. Authorization
	approved, err := m.authorizeAction(ctx, actionName, fullCmd, reason)
	if err != nil {
		return nil, err
	}
	if !approved {
		return nil, tools.ErrUserDeclined
	}

	// 2. Logging
	m.logToolAction("Running %s: %s", strings.ToLower(actionName), fullCmd)

	// 3. Telemetry/Heartbeat (Safe concurrency)
	defer telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)()

	// 4. Execution
	return m.executor.Execute(ctx, command, args...)
}

func newDevManager(sm devSecurity, validator domain_security.CommandValidator, runner *toolchain.GoRunner, opts ...devOption) *devManager {
	m := &devManager{
		sm:                sm,
		validator:         validator,
		executor:          &realExecutor{},
		runner:            runner,
		createTempFile:    os.CreateTemp,
		heartbeatInterval: 2 * time.Second, // Default
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

type devSecurity interface {
	domain_security.ActionConfirmer
	domain_security.Auditor
	domain_security.TerminalController
}
