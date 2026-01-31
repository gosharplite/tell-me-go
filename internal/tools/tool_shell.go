// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
)

type ShellTool struct {
	sm        *SecurityManager
	validator *CommandValidator
	executor  *ProcessExecutor
}

func NewShellTool(sm *SecurityManager) *ShellTool {
	return &ShellTool{
		sm:        sm,
		validator: NewCommandValidator(sm),
		executor:  NewProcessExecutor(),
	}
}

func (t *ShellTool) ExecuteCommand(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Command    string `json:"command"`
		Reason     string `json:"reason"`
		OutputFile string `json:"output_file"`
		Append     bool   `json:"append"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	command := params.Command
	if command == "" {
		return types.ToolResult{}, fmt.Errorf("command argument is required")
	}

	outputFile := params.OutputFile
	if outputFile != "" {
		resolvedFile, err := t.sm.IsPathWritable(outputFile)
		if err != nil {
			return types.ToolResult{}, err
		}
		outputFile = resolvedFile
	}

	// 1. Validate
	safe, _ := t.validator.IsSafe(command)
	approved := false

	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Execution auto-approved (bypass_confirmation enabled).\033[0m\n")
		approved = true
	} else if safe {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe command detected.\033[0m\n")
		approved = true
	} else {
		// 2. Authorize
		fmt.Fprintf(os.Stderr, "\033[0;36mExecute Command: \033[0m%s\n", command)
		if params.Reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", params.Reason)
		}
		if outputFile != "" {
			redir := ">"
			if params.Append {
				redir = ">>"
			}
			fmt.Fprintf(os.Stderr, "\033[0;34mRedirect: %s %s\033[0m\n", redir, outputFile)
		}
		fmt.Fprintf(os.Stderr, "⚠️  Execute this command? (y/N) ")

		char, err := t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char == "y" {
			approved = true
		}
	}

	if !approved {
		return types.ToolResult{Text: fmt.Sprintf("User denied execution of command: %s", command)}, nil
	}

	t.sm.logAudit("REASON", params.Reason, "COMMAND", command)

	// 3. Execute
	parts, err := t.validator.Split(command)
	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error parsing command: %v", err)}, nil
	}

	fmt.Fprintf(os.Stderr, "\033[90mExecuting... (Output shown below)\033[0m\n")
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	res, err := t.executor.RunCommand(ctx, parts, ExecutionConfig{
		OutputFile: outputFile,
		Append:     params.Append,
		Feedback:   os.Stderr,
	})
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	if err != nil {
		return types.ToolResult{}, err
	}

	output := res.Output
	if len(output) > 50000 {
		output = output[:50000] + "\n... (truncated)"
	}

	return types.ToolResult{
		Text: fmt.Sprintf("Exit Code: %d\nOutput:\n%s", res.ExitCode, output),
	}, nil
}

func (t *ShellTool) PipeCommands(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Commands   []string `json:"commands"`
		Reason     string   `json:"reason"`
		OutputFile string   `json:"output_file"`
		Append     bool     `json:"append"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	commands := params.Commands
	if len(commands) < 2 {
		return types.ToolResult{}, fmt.Errorf("at least two commands are required for piping")
	}

	outputFile := params.OutputFile
	if outputFile != "" {
		resolvedFile, err := t.sm.IsPathWritable(outputFile)
		if err != nil {
			return types.ToolResult{}, err
		}
		outputFile = resolvedFile
	}

	// 1. Validate
	allSafe := true
	for _, cmd := range commands {
		if safe, _ := t.validator.IsSafe(cmd); !safe {
			allSafe = false
			break
		}
	}

	approved := false
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Pipeline auto-approved (bypass_confirmation enabled).\033[0m\n")
		approved = true
	} else if allSafe {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe pipeline detected.\033[0m\n")
		approved = true
	} else {
		// 2. Authorize
		fmt.Fprintf(os.Stderr, "\033[0;36mExecute Pipeline: \033[0m%s\n", strings.Join(commands, " | "))
		if params.Reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", params.Reason)
		}
		if outputFile != "" {
			redir := ">"
			if params.Append {
				redir = ">>"
			}
			fmt.Fprintf(os.Stderr, "\033[0;34mRedirect Final Output: %s %s\033[0m\n", redir, outputFile)
		}
		fmt.Fprintf(os.Stderr, "⚠️  Execute this pipeline? (y/N) ")

		char, err := t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char == "y" {
			approved = true
		}
	}

	if !approved {
		return types.ToolResult{Text: "User denied execution of pipeline."}, nil
	}

	t.sm.logAudit("REASON", params.Reason, "PIPELINE", strings.Join(commands, " | "))

	// 3. Execute
	pipedParts := make([][]string, len(commands))
	for i, cmdStr := range commands {
		parts, err := t.validator.Split(cmdStr)
		if err != nil {
			return types.ToolResult{Text: fmt.Sprintf("Error parsing command at index %d: %v", i, err)}, nil
		}
		pipedParts[i] = parts
	}

	fmt.Fprintf(os.Stderr, "\033[90mExecuting Pipeline... (Output shown below)\033[0m\n")
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	res, err := t.executor.RunPipeline(ctx, pipedParts, ExecutionConfig{
		OutputFile: outputFile,
		Append:     params.Append,
		Feedback:   os.Stderr,
	})
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	if err != nil {
		return types.ToolResult{}, err
	}

	finalRes := res.Output
	if len(finalRes) > 50000 {
		finalRes = finalRes[:50000] + "\n... (truncated)"
	}

	return types.ToolResult{
		Text: fmt.Sprintf("Pipeline result. Exit Code: %d\n%s", res.ExitCode, finalRes),
	}, nil
}
