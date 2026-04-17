// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// psAliases is the set of command names that indicate PowerShell usage.
var psAliases = map[string]bool{
	"ps": true, "kill": true, "cat": true, "curl": true, "wget": true,
}

// psSubstringIndicators are substrings in the full command that indicate PowerShell usage.
var psSubstringIndicators = []string{
	"$env:", "$(", "select-string", "where-object", "foreach-object",
}

type commandTranslator interface {
	Translate(parts []string) []string
}

type posixTranslator struct{}

func (p *posixTranslator) Translate(parts []string) []string {
	return parts
}

type windowsTranslator struct{}

func (w *windowsTranslator) Translate(parts []string) []string {
	if len(parts) == 0 {
		return parts
	}

	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "ls":
		return w.translateLS(args)
	case "rm":
		return w.translateRM(args)
	case "mkdir":
		return w.translateMkdir(args)
	case "cp":
		return w.translateCP(args)
	case "mv":
		return w.translateMV(args)
	}

	return parts
}

func (w *windowsTranslator) translateRM(args []string) []string {
	isRecursive := false
	for _, arg := range args {
		if arg == "-r" || arg == "-rf" {
			isRecursive = true
			break
		}
	}

	filteredArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-r" || arg == "-rf" || arg == "-f" || arg == "-v" {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}

	if isRecursive {
		return append([]string{"cmd", "/c", "rd", "/s", "/q"}, filteredArgs...)
	}
	return append([]string{"cmd", "/c", "del", "/f", "/q"}, filteredArgs...)
}

func (w *windowsTranslator) translateMkdir(args []string) []string {
	filteredArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-p" {
			continue
		}
		filteredArgs = append(filteredArgs, arg)
	}
	return append([]string{"cmd", "/c", "mkdir"}, filteredArgs...)
}

func (w *windowsTranslator) translateCP(args []string) []string {
	return append([]string{"cmd", "/c", "copy"}, args...)
}

func (w *windowsTranslator) translateMV(args []string) []string {
	return append([]string{"cmd", "/c", "move"}, args...)
}

type shellWrapper interface {
	Wrap(command string, parts []string) []string
}

type posixShellWrapper struct{}

func (p *posixShellWrapper) Wrap(command string, parts []string) []string {
	return []string{"sh", "-c", command}
}

type windowsShellWrapper struct{}

func (w *windowsShellWrapper) Wrap(command string, parts []string) []string {
	// Windows-specific selection: Prefer PowerShell/pwsh for cmdlets or PS indicators.
	if w.isPowerShellIndicator(command, parts) {
		shell := "powershell"
		// Prefer pwsh (Core) over powershell (Desktop) if available.
		if p, err := exec.LookPath("pwsh"); err == nil && p != "" {
			shell = "pwsh"
		}
		// Force UTF-8 output encoding to avoid mojibake on localized systems.
		// Use -NoProfile and -NonInteractive for stability and performance.
		return []string{
			shell,
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command,
		}
	}

	return []string{"cmd.exe", "/c", command}
}

func (w *windowsShellWrapper) isPowerShellIndicator(command string, parts []string) bool {
	if len(parts) == 0 {
		return false
	}

	first := parts[0]

	// 1. Check for common PowerShell aliases
	if psAliases[strings.ToLower(first)] {
		return true
	}

	// 2. Check for PowerShell Verb-Noun pattern (e.g. "Get-ChildItem")
	if dashIdx := strings.Index(first, "-"); dashIdx > 0 && dashIdx < len(first)-1 {
		return true
	}

	// 3. Check for PowerShell-specific substrings in the full command
	lower := strings.ToLower(command)
	for _, ind := range psSubstringIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}

	return false
}

type shellTool struct {
	sm         shellSecurity
	validator  domain_security.CommandValidator
	executor   *processExecutor
	translator commandTranslator
	wrapper    shellWrapper
	maxOutput  int
}

func newshellTool(sm shellSecurity, validator domain_security.CommandValidator, translator commandTranslator, wrapper shellWrapper) *shellTool {
	return &shellTool{
		sm:         sm,
		validator:  validator,
		executor:   newprocessExecutor(),
		translator: translator,
		wrapper:    wrapper,
		maxOutput:  50000,
	}
}

type executeParams struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	Reason     string            `json:"reason"`
	OutputFile string            `json:"output_file"`
	Append     bool              `json:"append"`
	Timeout    int               `json:"timeout"`
}

func (t *shellTool) ExecuteCommand(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params executeParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	parts, displayCmd, err := t.prepareExecutionParts(params)
	if err != nil {
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	outputFile, approved, err := t.authorizeAndAudit(ctx, params, displayCmd)
	if err != nil || !approved {
		return t.handleAuthResult(approved, err, "command: "+displayCmd)
	}

	stopHB := t.startHeartbeat(hb)
	defer stopHB()

	// Set default timeout to 15s if not provided
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 15
	}

	res, err := t.runWithFeedback(ctx, "Executing", func() (executionResult, error) {
		// Enforce timeout via context
		tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		return t.executor.RunCommand(tCtx, parts, executionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   &warnWriter{sm: t.sm},
			MaxCapture: t.maxOutput,
			Env:        params.Env,
		})
	})

	if err != nil {
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	return tools.ToolResult{Text: t.formatResult(res, false)}, nil
}

func (t *shellTool) prepareExecutionParts(params executeParams) ([]string, string, error) {
	if len(params.Args) > 0 {
		parts := make([]string, len(params.Args))
		for i, arg := range params.Args {
			parts[i] = filepath.FromSlash(arg)
		}
		return t.translator.Translate(parts), strings.Join(params.Args, " "), nil
	}

	if params.Command == "" {
		return nil, "", fmt.Errorf("command or args is required")
	}

	parts, err := t.prepareCommand(params.Command)
	if err != nil {
		return nil, "", err
	}
	return parts, params.Command, nil
}

func (t *shellTool) authorizeAndAudit(ctx context.Context, params executeParams, displayCommand string) (string, bool, error) {
	outputFile, err := t.resolveOutputFile(params.OutputFile)
	if err != nil {
		return "", false, err
	}

	safe, _ := t.validator.IsSafe(displayCommand)
	approved, err := t.authorize(ctx, "Command", displayCommand, params.Reason, safe, outputFile, params.Append)
	if err != nil || !approved {
		return outputFile, approved, err
	}

	t.auditExecution(displayCommand, params.Reason, params.OutputFile, params.Append)
	return outputFile, true, nil
}

func (t *shellTool) PipeCommands(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Commands   []string `json:"commands"`
		Reason     string   `json:"reason"`
		OutputFile string   `json:"output_file"`
		Append     bool     `json:"append"`
		Timeout    int      `json:"timeout"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if len(params.Commands) < 2 {
		return tools.ToolResult{Error: fmt.Errorf("at least two commands are required for piping"), Text: "at least two commands are required for piping"}, nil
	}

	outputFile, err := t.resolveOutputFile(params.OutputFile)
	if err != nil {
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
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
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	stopHB := t.startHeartbeat(hb)
	defer stopHB()

	// Set default timeout to 15s if not provided
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 15
	}

	res, err := t.runWithFeedback(ctx, "Executing Pipeline", func() (executionResult, error) {
		// Enforce timeout via context
		tCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		feedback := &warnWriter{sm: t.sm}
		return t.executor.RunPipeline(tCtx, pipedParts, executionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   feedback,
			MaxCapture: t.maxOutput,
		})
	})

	if err != nil {
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
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
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
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
	// 1. Lock to print the header
	t.sm.TerminalLock()
	t.sm.Warn(fmt.Sprintf("%s... (Output shown below)", msg))
	t.sm.Warn("------------------------------------------------------------")
	t.sm.TerminalUnlock()

	// 2. Execute command WITHOUT holding the lock to allow spinner to run
	res, err := runFn()

	// 3. Lock to print the footer
	t.sm.TerminalLock()
	t.sm.Warn("------------------------------------------------------------")
	t.sm.TerminalUnlock()

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
	w.sm.TerminalLock()
	defer w.sm.TerminalUnlock()

	// The process executor includes newlines in the feedback messages.
	// Since Warn() also typically adds a newline, we trim one from the end
	// to prevent double-spacing in the terminal.
	msg := string(p)
	w.sm.Warn(strings.TrimSuffix(msg, "\n"))
	return len(p), nil
}

func (t *shellTool) prepareCommand(command string) ([]string, error) {
	parts, err := t.validator.Split(command)
	if err != nil {
		return nil, fmt.Errorf("error parsing command: %w", err)
	}

	// Automatically wrap in shell if shell features are detected (operators, wildcards, interpolation, cmdlets)
	if t.validator.HasShellFeatures(parts) {
		parts = t.wrapper.Wrap(command, parts)
	}

	if err := t.validator.ValidateStructure(parts); err != nil {
		return nil, err
	}

	return parts, nil
}

func (t *shellTool) startHeartbeat(hb chan<- struct{}) (stop func()) {
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
					sendHeartbeat(context.Background(), hb)
				}
			}
		}
	}()

	return func() {
		close(done)
	}
}

func (t *shellTool) auditExecution(command, reason, outputFile string, isAppend bool) {
	argsAudit := []any{
		"REASON", reason,
		"COMMAND", command,
	}
	if outputFile != "" {
		argsAudit = append(argsAudit, "OUTPUT_FILE", outputFile, "APPEND", isAppend)
	}
	t.sm.LogAudit("EXECUTE_COMMAND", argsAudit...)
}

// parseLSShortFlags parses combined short flags (e.g., "-laR") and updates the flags.
func parseLSShortFlags(flags string, recursive, showAll *bool) {
	for _, c := range flags {
		switch c {
		case 'R':
			*recursive = true
		case 'a':
			*showAll = true
		}
	}
}

func (w *windowsTranslator) translateLS(args []string) []string {
	recursive := false
	showAll := false
	var paths []string

	for _, arg := range args {
		switch {
		case arg == "--recursive":
			recursive = true
		case arg == "--all":
			showAll = true
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			parseLSShortFlags(arg[1:], &recursive, &showAll)
		case !strings.HasPrefix(arg, "-"):
			paths = append(paths, arg)
		}
	}

	translated := []string{"cmd", "/c", "dir"}
	if recursive {
		translated = append(translated, "/S")
	}
	if showAll {
		translated = append(translated, "/A")
	}
	translated = append(translated, paths...)
	return translated
}
