// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type shellTool struct {
	sm        shellSecurity
	validator domain_security.CommandValidator
	executor  *processExecutor
	maxOutput int
}

func newshellTool(sm shellSecurity, validator domain_security.CommandValidator) *shellTool {
	return &shellTool{
		sm:        sm,
		validator: validator,
		executor:  newprocessExecutor(),
		maxOutput: 50000,
	}
}

func (t *shellTool) ExecuteCommand(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Command    string `json:"command"`
		Reason     string `json:"reason"`
		OutputFile string `json:"output_file"`
		Append     bool   `json:"append"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

	// NEW: Automatically wrap in shell if shell features are detected (operators, wildcards, interpolation)
	// This prevents the AI from failing on valid shell commands that don't fit the direct-exec model.
	if t.validator.HasShellFeatures(parts) {
		if runtime.GOOS == "windows" {
			parts = []string{"cmd.exe", "/c", params.Command}
		} else {
			parts = []string{"sh", "-c", params.Command}
		}
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

	argsAudit := []any{
		"REASON", params.Reason,
		"COMMAND", params.Command,
	}
	if params.OutputFile != "" {
		argsAudit = append(argsAudit, "OUTPUT_FILE", params.OutputFile, "APPEND", params.Append)
	}
	t.sm.LogAudit("EXECUTE_COMMAND", argsAudit...)

	// 3. Execute
	feedback := &warnWriter{sm: t.sm}

	// Heartbeat while command is running
	done := make(chan struct{})
	go func() {
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
	}()

	res, err := t.runWithFeedback(ctx, "Executing", func() (executionResult, error) {
		return t.executor.RunCommand(ctx, parts, executionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   feedback,
			MaxCapture: t.maxOutput,
		})
	})
	close(done)

	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: t.formatResult(res, false)}, nil
}

func (t *shellTool) PipeCommands(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Commands   []string `json:"commands"`
		Reason     string   `json:"reason"`
		OutputFile string   `json:"output_file"`
		Append     bool     `json:"append"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

	argsAudit := []any{
		"PIPELINE", pipelineStr,
		"REASON", params.Reason,
	}
	if params.OutputFile != "" {
		argsAudit = append(argsAudit, "OUTPUT_FILE", params.OutputFile, "APPEND", params.Append)
	}
	t.sm.LogAudit("PIPE_COMMANDS", argsAudit...)

	// 2. Execute
	pipedParts, err := t.splitPipeline(params.Commands)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// Heartbeat while pipeline is running
	done := make(chan struct{})
	go func() {
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
	}()

	res, err := t.runWithFeedback(ctx, "Executing Pipeline", func() (executionResult, error) {
		feedback := &warnWriter{sm: t.sm}
		return t.executor.RunPipeline(ctx, pipedParts, executionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   feedback,
			MaxCapture: t.maxOutput,
		})
	})
	close(done)

	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: t.formatResult(res, true)}, nil
}

func (t *shellTool) isPipelineSafe(commands []string) bool {
	for _, cmd := range commands {
		if safe, _ := t.validator.IsSafe(cmd); !safe {
			return false
		}
	}
	return true
}

func (t *shellTool) splitPipeline(commands []string) ([][]string, error) {
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

func (t *shellTool) handleAuthResult(approved bool, err error, label string) (tools.ToolResult, error) {
	if err != nil {
		return tools.ToolResult{}, err
	}
	return t.deniedResult(label), tools.ErrUserDeclined
}

func (t *shellTool) authorize(ctx context.Context, label, detail, reason string, isSafe bool, outputFile string, append bool) (bool, error) {
	if outputFile != "" {
		mode := " > "
		if append {
			mode = " >> "
		}
		detail = fmt.Sprintf("%s%s%s", detail, mode, outputFile)
	}
	return t.sm.Authorize(ctx, label, detail, reason, isSafe)
}

func (t *shellTool) runWithFeedback(ctx context.Context, msg string, runFn func() (executionResult, error)) (executionResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	t.sm.Warn(fmt.Sprintf("%s... (Output shown below)", msg))
	t.sm.Warn("------------------------------------------------------------")
	res, err := runFn()
	t.sm.Warn("------------------------------------------------------------")
	return res, err
}

func (t *shellTool) formatResult(res executionResult, isPipeline bool) string {
	output := res.Output
	if res.Truncated {
		output += "\n... (truncated)"
	}
	if isPipeline {
		return fmt.Sprintf("Pipeline result. Exit Code: %d\n%s", res.ExitCode, output)
	}
	return fmt.Sprintf("Exit Code: %d\nOutput:\n%s", res.ExitCode, output)
}

func (t *shellTool) resolveOutputFile(path string) (string, error) {
	// Hardened sanitation: trim whitespace and remove null bytes
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\x00", "")

	if path == "" {
		return "", nil
	}
	return t.sm.IsPathWritable(path)
}

func (t *shellTool) deniedResult(label string) tools.ToolResult {
	return tools.ToolResult{Text: fmt.Sprintf("User denied execution of %s.", label)}
}

type shellSecurity interface {
	domain_security.TerminalController
	domain_security.Auditor
	domain_security.PathValidator
	domain_security.ActionConfirmer
}

type warnWriter struct {
	sm shellSecurity
}

func (w *warnWriter) Write(p []byte) (n int, err error) {
	// The process executor includes newlines in the feedback messages.
	// Since Warn() also typically adds a newline, we trim one from the end
	// to prevent double-spacing in the terminal.
	msg := string(p)
	w.sm.Warn(strings.TrimSuffix(msg, "\n"))
	return len(p), nil
}
