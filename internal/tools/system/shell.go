// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package system

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type ShellTool struct {
	sm        *security.SecurityManager
	validator *framework.CommandValidator
	executor  *ProcessExecutor
}

func NewShellTool(sm *security.SecurityManager) *ShellTool {
	return &ShellTool{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
		executor:  NewProcessExecutor(),
	}
}

const maxShellOutput = 50000

func (t *ShellTool) ExecuteCommand(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Command    string `json:"command"`
		Reason     string `json:"reason"`
		OutputFile string `json:"output_file"`
		Append     bool   `json:"append"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Command == "" {
		return tools.ToolResult{}, fmt.Errorf("command argument is required")
	}

	outputFile, err := t.resolveOutputFile(params.OutputFile)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 1. Authorize
	safe, _ := t.validator.IsSafe(params.Command)
	approved, err := t.authorize(ctx, "Command", params.Command, params.Reason, safe, outputFile, params.Append)
	if err != nil || !approved {
		return t.handleAuthResult(approved, err, "command: "+params.Command)
	}

	t.sm.LogAudit("REASON", params.Reason, "COMMAND", params.Command)

	// 2. Execute
	parts, err := t.validator.Split(params.Command)
	if err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error parsing command: %v", err)}, nil
	}

	res, err := t.runWithFeedback(ctx, "Executing", func() (ExecutionResult, error) {
		return t.executor.RunCommand(ctx, parts, ExecutionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   os.Stderr,
			MaxCapture: maxShellOutput,
		})
	})

	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: t.formatResult(res, false)}, nil
}

func (t *ShellTool) PipeCommands(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Commands   []string `json:"commands"`
		Reason     string   `json:"reason"`
		OutputFile string   `json:"output_file"`
		Append     bool     `json:"append"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if len(params.Commands) < 2 {
		return tools.ToolResult{}, fmt.Errorf("at least two commands are required for piping")
	}

	outputFile, err := t.resolveOutputFile(params.OutputFile)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 1. Authorize
	safe := t.isPipelineSafe(params.Commands)
	pipelineStr := strings.Join(params.Commands, " | ")
	approved, err := t.authorize(ctx, "Pipeline", pipelineStr, params.Reason, safe, outputFile, params.Append)
	if err != nil || !approved {
		return t.handleAuthResult(approved, err, "pipeline")
	}

	t.sm.LogAudit("PIPELINE", pipelineStr, "REASON", params.Reason)

	// 2. Execute
	pipedParts, err := t.splitPipeline(params.Commands)
	if err != nil {
		return tools.ToolResult{Text: err.Error()}, nil
	}

	res, err := t.runWithFeedback(ctx, "Executing Pipeline", func() (ExecutionResult, error) {
		return t.executor.RunPipeline(ctx, pipedParts, ExecutionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   os.Stderr,
			MaxCapture: maxShellOutput,
		})
	})

	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: t.formatResult(res, true)}, nil
}

func (t *ShellTool) isPipelineSafe(commands []string) bool {
	for _, cmd := range commands {
		if safe, _ := t.validator.IsSafe(cmd); !safe {
			return false
		}
	}
	return true
}

func (t *ShellTool) splitPipeline(commands []string) ([][]string, error) {
	pipedParts := make([][]string, len(commands))
	for i, cmdStr := range commands {
		parts, err := t.validator.Split(cmdStr)
		if err != nil {
			return nil, fmt.Errorf("error parsing command at index %d: %v", i, err)
		}
		pipedParts[i] = parts
	}
	return pipedParts, nil
}

func (t *ShellTool) handleAuthResult(approved bool, err error, label string) (tools.ToolResult, error) {
	if err != nil {
		return tools.ToolResult{}, err
	}
	return t.deniedResult(label), nil
}

func (t *ShellTool) authorize(ctx context.Context, label, detail, reason string, isSafe bool, outputFile string, append bool) (bool, error) {
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] %s auto-approved (bypass_confirmation enabled).\033[0m\n", label)
		return true, nil
	}
	if isSafe {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe %s detected.\033[0m\n", strings.ToLower(label))
		return true, nil
	}

	fmt.Fprintf(os.Stderr, "\033[0;36mExecute %s: \033[0m%s\n", label, detail)
	if reason != "" {
		fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
	}
	if outputFile != "" {
		redir := ">"
		if append {
			redir = ">>"
		}
		fmt.Fprintf(os.Stderr, "\033[0;34mRedirect: %s %s\033[0m\n", redir, outputFile)
	}
	fmt.Fprintf(os.Stderr, "⚠️  Execute this %s? (y/N) ", strings.ToLower(label))

	char, err := t.sm.ReadSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return false, err
	}
	return char == "y", nil
}

func (t *ShellTool) runWithFeedback(ctx context.Context, msg string, runFn func() (ExecutionResult, error)) (ExecutionResult, error) {
	fmt.Fprintf(os.Stderr, "\033[90m%s... (Output shown below)\033[0m\n", msg)
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")
	res, err := runFn()
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")
	return res, err
}

func (t *ShellTool) formatResult(res ExecutionResult, isPipeline bool) string {
	output := res.Output
	if len(output) >= maxShellOutput {
		output += "\n... (truncated)"
	}
	if isPipeline {
		return fmt.Sprintf("Pipeline result. Exit Code: %d\n%s", res.ExitCode, output)
	}
	return fmt.Sprintf("Exit Code: %d\nOutput:\n%s", res.ExitCode, output)
}

func (t *ShellTool) resolveOutputFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return t.sm.IsPathWritable(path)
}

func (t *ShellTool) deniedResult(label string) tools.ToolResult {
	return tools.ToolResult{Text: fmt.Sprintf("User denied execution of %s.", label)}
}
