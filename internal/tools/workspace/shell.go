// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
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

// rmFlagsToStrip is the set of POSIX rm flags that should be removed during Windows translation.
var rmFlagsToStrip = map[string]bool{
	"-r": true, "-rf": true, "-f": true, "-v": true,
}

// rmRecursiveFlags is the subset of rm flags that indicate recursive deletion.
var rmRecursiveFlags = map[string]bool{
	"-r": true, "-rf": true,
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
	filteredArgs := make([]string, 0, len(args))

	for _, arg := range args {
		if rmFlagsToStrip[arg] {
			if rmRecursiveFlags[arg] {
				isRecursive = true
			}
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

type windowsShellWrapper struct {
	validator domain_security.CommandValidator
}

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

	// 0. Bare newlines require PowerShell: cmd.exe /c does not treat embedded LF
	// as a command separator; subsequent lines are silently dropped.
	// PowerShell's -Command handles multi-statement newlines correctly.
	// Use the quote-aware validator to avoid false positives on safely quoted
	// newlines (e.g. echo "a\nb" is a single command, not multi-statement).
	if w.validator != nil && w.validator.HasBareNewline(command) {
		return true
	}

	first := parts[0]

	// 1. Check for common PowerShell aliases
	if psAliases[strings.ToLower(first)] {
		return true
	}

	// 2. Check for PowerShell Verb-Noun pattern (e.g. "Get-ChildItem")
	// Must be a bare command name, not a filesystem path containing hyphens.
	if dashIdx := strings.Index(first, "-"); dashIdx > 0 && dashIdx < len(first)-1 {
		// Only match if the segment before the hyphen contains no path separators
		// or dots (to avoid false positives on paths like "tell-me-go/helper").
		prefix := first[:dashIdx]
		if !strings.ContainsAny(prefix, "/\\.") {
			return true
		}
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
	sm                shellSecurity
	eventBus          events.EventBus
	validator         domain_security.CommandValidator
	executor          *processExecutor
	translator        commandTranslator
	wrapper           shellWrapper
	maxOutput         int
	heartbeatInterval time.Duration // zero means default (2s)
}

func newshellTool(sm shellSecurity, eventBus events.EventBus, validator domain_security.CommandValidator, translator commandTranslator, wrapper shellWrapper) *shellTool {
	return &shellTool{
		sm:         sm,
		eventBus:   eventBus,
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

	// Build the actual executed command string for audit fidelity.
	// When prepareCommand wraps the command (e.g. sh -c "..."), parts
	// reflects what is actually executed.
	executedCmdStr := strings.Join(parts, " ")

	outputFile, approved, err := t.authorizeAndAudit(ctx, params, displayCmd, executedCmdStr)
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
			Feedback:   &warnWriter{eventBus: t.eventBus},
			MaxCapture: t.maxOutput,
			Env:        params.Env,
		})
	})

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return t.formatTimeoutResult(res, timeout), nil
		}
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	return tools.ToolResult{Text: t.formatResult(res, false)}, nil
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\n\r\"'|&;<>()$`\\*?[]#~=") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func (t *shellTool) prepareExecutionParts(params executeParams) ([]string, string, error) {
	if len(params.Args) > 0 {
		parts := make([]string, len(params.Args))
		displayParts := make([]string, len(params.Args))
		for i, arg := range params.Args {
			parts[i] = filepath.FromSlash(arg)
			displayParts[i] = quoteArg(params.Args[i])
		}

		return t.translator.Translate(parts), strings.Join(displayParts, " "), nil
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

func (t *shellTool) authorizeAndAudit(ctx context.Context, params executeParams, displayCommand string, executedCommand string) (string, bool, error) {
	outputFile, err := t.resolveOutputFile(params.OutputFile)
	if err != nil {
		return "", false, err
	}

	safe, _ := t.validator.IsSafe(displayCommand)
	approved, err := t.authorize(ctx, "Command", displayCommand, params.Reason, safe, outputFile, params.Append)
	if err != nil || !approved {
		return outputFile, approved, err
	}

	t.auditExecution(displayCommand, executedCommand, params.Reason, params.OutputFile, params.Append)
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

	outputFile, approved, err := t.authorizeAndAuditPipeline(ctx, params.Commands, params.Reason, params.OutputFile, params.Append)
	if err != nil || !approved {
		return t.handleAuthResult(approved, err, "pipeline")
	}

	// Execute
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

		feedback := &warnWriter{eventBus: t.eventBus}
		return t.executor.RunPipeline(tCtx, pipedParts, executionConfig{
			OutputFile: outputFile,
			Append:     params.Append,
			Feedback:   feedback,
			MaxCapture: t.maxOutput,
		})
	})

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return t.formatTimeoutResult(res, timeout), nil
		}
		return tools.ToolResult{Error: err, Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	return tools.ToolResult{Text: t.formatResult(res, true)}, nil
}

func (t *shellTool) authorizeAndAuditPipeline(ctx context.Context, commands []string, reason, outputFile string, isAppend bool) (string, bool, error) {
	if len(commands) < 2 {
		return "", false, fmt.Errorf("at least two commands are required for piping")
	}

	resolvedFile, err := t.resolveOutputFile(outputFile)
	if err != nil {
		return "", false, err
	}

	safe := t.isPipelineSafe(commands)
	pipelineStr := strings.Join(commands, " | ")
	approved, err := t.authorize(ctx, "Pipeline", pipelineStr, reason, safe, resolvedFile, isAppend)
	if err != nil || !approved {
		return resolvedFile, approved, err
	}

	argsAudit := []any{
		"PIPELINE", pipelineStr,
		"REASON", reason,
	}
	if outputFile != "" {
		argsAudit = append(argsAudit, "OUTPUT_FILE", outputFile, "APPEND", isAppend)
	}
	t.sm.LogAudit("PIPE_COMMANDS", argsAudit...)

	return resolvedFile, true, nil
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
		// Reject bare newlines early: pipeline segments are contractually
		// single commands; a bare newline would silently fuse into a
		// multi-command injection under sh -c.
		if t.validator.HasBareNewline(cmdStr) {
			return nil, fmt.Errorf("invalid command at index %d: pipeline segment contains unquoted bare newline", i)
		}

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
	if t.eventBus != nil {
		_ = t.eventBus.Publish(ctx, events.ToolOutputStreamEvent{
			Message: fmt.Sprintf("%s... (Output shown below)", msg),
			Level:   "info",
		})
		_ = t.eventBus.Publish(ctx, events.ToolOutputStreamEvent{
			Message: "------------------------------------------------------------",
			Level:   "info",
		})
	}

	// Execute command — no terminal lock needed; EventBus handles rendering.
	res, err := runFn()

	if t.eventBus != nil {
		_ = t.eventBus.Publish(ctx, events.ToolOutputStreamEvent{
			Message: "------------------------------------------------------------",
			Level:   "info",
		})
	}

	return res, err
}

func (t *shellTool) formatTimeoutResult(res executionResult, timeout int) tools.ToolResult {
	text := fmt.Sprintf(
		"Error: command timed out after %ds (tool-enforced limit; the process tree was terminated). "+
			"If more time is needed, retry with a larger 'timeout' parameter (e.g., timeout: 120).",
		timeout)
	if res.Output != "" {
		text += "\n\nPartial output before timeout:\n" + res.Output
	}
	return tools.ToolResult{Error: context.DeadlineExceeded, Text: text}
}

func (t *shellTool) formatResult(res executionResult, isPipeline bool) string {
	output := res.Output
	if res.Truncated {
		output += "\n... (truncated) - use output_file parameter to capture full output"
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
	domain_security.Auditor
	domain_security.PathValidator
	domain_security.ActionConfirmer
}

type warnWriter struct {
	eventBus events.EventBus
}

func (w *warnWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSuffix(string(p), "\n")
	if w.eventBus != nil {
		_ = w.eventBus.Publish(context.Background(), events.ToolOutputStreamEvent{
			Message: msg,
			Level:   "info",
		})
	}
	return len(p), nil
}

func (t *shellTool) prepareCommand(command string) ([]string, error) {
	parts, err := t.validator.Split(command)
	if err != nil {
		return nil, fmt.Errorf("error parsing command: %w", err)
	}

	// Automatically wrap in shell if shell features are detected (operators, wildcards, interpolation, cmdlets)
	// or if residual bare newlines remain after normalization (which act as command separators under sh -c).
	if t.validator.HasShellFeatures(parts) || t.validator.HasBareNewline(command) {
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
		interval := t.heartbeatInterval
		if interval <= 0 {
			interval = 2 * time.Second
		}
		ticker := time.NewTicker(interval)
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

func (t *shellTool) auditExecution(command, executedCommand, reason, outputFile string, isAppend bool) {
	argsAudit := []any{
		"REASON", reason,
		"COMMAND", command,
	}
	if command != executedCommand {
		argsAudit = append(argsAudit, "EXECUTED", executedCommand)
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
