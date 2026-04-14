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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
	"github.com/gosharplite/tell-me-go/internal/pkg/telemetry"
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
	CheckGovulncheck(ctx context.Context) error
	RunModTidy(ctx context.Context) ([]byte, error)
	FormatCode(ctx context.Context, path string) ([]byte, error)
	RunTests(ctx context.Context, path string) ([]byte, error)
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
}

type realExecutor struct{}

func (e *realExecutor) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (m *devManager) runTests(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	// Set default timeout if not provided
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 15
	}

	parts, err := m.validateTestCommand(params.Command)
	if err != nil {
		return tools.ToolResult{}, err
	}

	output, err := m.executeWithHeartbeat(
		ctx,
		hb,
		"run_tests",
		params.Command,
		"Executing project tests",
		func() ([]byte, error) {
			// Enforce timeout during execution phase
			tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
			defer cancel()
			return m.executor.Execute(tCtx, parts[0], parts[1:]...)
		},
	)

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

	res := formatExecutionResult("Test execution", output, err, 100, "")
	if res.Error != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return tools.ToolResult{Text: res.Error.Error()}, nil
		}
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
	var out []byte
	err := m.runWithHeartbeat(ctx, hb, "go_tidy", command, "Tidying project dependencies and formatting", func() error {
		var err error
		out, err = m.runner.RunModTidy(ctx)
		if err != nil {
			return err
		}
		out, err = m.runner.FormatCode(ctx, "./...")
		return err
	})

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

	if err != nil {
		// Because we combined them, we need to decide which displayName to use.
		// For simplicity, we can use "Go mod tidy/fmt".
		res := formatExecutionResult("Go mod tidy/fmt", out, err, 50, "")
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
	var report toolchain.CoverageReport
	err := m.runWithHeartbeat(ctx, hb, "get_coverage", command, "Getting test coverage summary", func() error {
		var err error
		report, err = m.runner.RunTestsWithCoverage(ctx, path, false, "")
		return err
	})

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

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
	var out string
	var tool string
	err := m.runWithHeartbeat(ctx, hb, "run_linter", "go lint", "Running code analysis", func() error {
		var err error
		out, tool, err = m.runner.RunLinter(ctx)
		return err
	})

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

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
	var outStr string
	err := m.runWithHeartbeat(ctx, hb, "run_benchmark", fullCmd, "Running project benchmarks", func() error {
		var err error
		outStr, err = m.runner.RunBenchmarks(ctx, path, bench)
		return err
	})

	if errors.Is(err, tools.ErrUserDeclined) {
		return tools.ToolResult{Text: "Action denied by user."}, err
	}

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
	if err := m.runner.CheckGovulncheck(ctx); err != nil {
		return tools.ToolResult{}, err
	}

	command := "govulncheck"
	argsList := []string{"./..."}
	fullCmd := command + " " + strings.Join(argsList, " ")

	out, err := m.executeWithHeartbeat(
		ctx,
		hb,
		"check_vulnerabilities",
		fullCmd,
		"Checking for known vulnerabilities",
		func() ([]byte, error) {
			return m.executor.Execute(ctx, command, argsList...)
		},
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

func (m *devManager) runWithHeartbeat(
	ctx context.Context,
	hb chan<- struct{},
	actionName, fullCmd, reason string,
	fn func() error,
) error {
	// 1. Authorization
	approved, err := m.authorizeAction(ctx, actionName, fullCmd, reason)
	if err != nil {
		return err
	}
	if !approved {
		return tools.ErrUserDeclined
	}

	// 2. Logging
	m.logToolAction("Running %s: %s", strings.ToLower(actionName), fullCmd)

	// 3. Telemetry/Heartbeat (Safe concurrency)
	defer telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)()

	// 4. Execution
	return fn()
}

func (m *devManager) executeWithHeartbeat(
	ctx context.Context,
	hb chan<- struct{},
	actionName, fullCmd, reason string,
	execute func() ([]byte, error),
) ([]byte, error) {
	var out []byte
	err := m.runWithHeartbeat(ctx, hb, actionName, fullCmd, reason, func() error {
		var err error
		out, err = execute()
		return err
	})
	return out, err
}

func newDevManager(sm devSecurity, validator domain_security.CommandValidator, runner goRunner, opts ...devOption) *devManager {
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
