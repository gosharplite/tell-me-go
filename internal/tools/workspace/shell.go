// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type ShellTool struct {
	sm        *security.SecurityManager
	validator *security.CommandValidator
	executor  *ProcessExecutor
	maxOutput int
}

func NewShellTool(sm *security.SecurityManager) *ShellTool {
	return &ShellTool{
		sm:        sm,
		validator: security.NewCommandValidator(sm, sm.GetInteractor()),
		executor:  NewProcessExecutor(),
		maxOutput: 50000,
	}
}


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

	// 1. Technical Validation: Split and check structure before authorization
	parts, err := t.validator.Split(params.Command)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("error parsing command: %w", err)
	}

	if err := t.validator.ValidateStructure(parts); err != nil {
		return tools.ToolResult{}, err
	}

	outputFile, err := t.resolveOutputFile(params.OutputFile)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 2. Authorize
	safe, _ := t.validator.IsSafe(params.Command)
	approved, err := t.authorize(ctx, "Command", params.Command, params.Reason, safe, outputFile, params.Append)
	if err != nil || !approved {
		return t.handleAuthResult(approved, err, "command: "+params.Command)
	}

	t.sm.LogAudit("REASON", params.Reason, "COMMAND", params.Command)

	// 3. Execute
	res, err := t.runWithFeedback(ctx, "Executing", func() (ExecutionResult, error) {
		return t.executor.RunCommand(ctx, parts, ExecutionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   os.Stderr,
			MaxCapture: t.maxOutput,
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
		return tools.ToolResult{}, err
	}

	res, err := t.runWithFeedback(ctx, "Executing Pipeline", func() (ExecutionResult, error) {
		return t.executor.RunPipeline(ctx, pipedParts, ExecutionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   os.Stderr,
			MaxCapture: t.maxOutput,
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
			return nil, fmt.Errorf("error parsing command at index %d: %w", i, err)
		}
		if err := t.validator.ValidateStructure(parts); err != nil {
			return nil, fmt.Errorf("invalid command at index %d: %w", i, err)
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
		t.sm.Warn(fmt.Sprintf("[Bypassed] %s auto-approved (bypass_confirmation enabled).", label))
		return true, nil
	}
	if isSafe {
		t.sm.Warn(fmt.Sprintf("[Auto-Approved] Safe %s detected.", strings.ToLower(label)))
		return true, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Execute %s: %s\n", label, detail))
	if reason != "" {
		sb.WriteString(fmt.Sprintf("Reason: %s\n", reason))
	}
	if outputFile != "" {
		redir := ">"
		if append {
			redir = ">>"
		}
		sb.WriteString(fmt.Sprintf("Redirect: %s %s\n", redir, outputFile))
	}
	sb.WriteString(fmt.Sprintf("⚠️  Execute this %s? (y/N) ", strings.ToLower(label)))

	return t.sm.GetInteractor().Confirm(ctx, sb.String())
}

func (t *ShellTool) runWithFeedback(ctx context.Context, msg string, runFn func() (ExecutionResult, error)) (ExecutionResult, error) {
	t.sm.Warn(fmt.Sprintf("%s... (Output shown below)", msg))
	t.sm.Warn("------------------------------------------------------------")
	res, err := runFn()
	t.sm.Warn("------------------------------------------------------------")
	return res, err
}

func (t *ShellTool) formatResult(res ExecutionResult, isPipeline bool) string {
	output := res.Output
	if res.Truncated {
		output += "\n... (truncated)"
	}
	if isPipeline {
		return fmt.Sprintf("Pipeline result. Exit Code: %d\n%s", res.ExitCode, output)
	}
	return fmt.Sprintf("Exit Code: %d\nOutput:\n%s", res.ExitCode, output)
}

func (t *ShellTool) resolveOutputFile(path string) (string, error) {
	// Hardened sanitation: trim whitespace and remove null bytes
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\x00", "")

	if path == "" {
		return "", nil
	}
	return t.sm.IsPathWritable(path)
}

func (t *ShellTool) deniedResult(label string) tools.ToolResult {
	return tools.ToolResult{Text: fmt.Sprintf("User denied execution of %s.", label)}
}
