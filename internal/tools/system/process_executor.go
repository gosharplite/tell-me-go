// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package system

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/gosharplite/tell-me-go/internal/ui/colors"
)

// ExecutionConfig defines parameters for command or pipeline execution.
type ExecutionConfig struct {
	OutputFile string
	Append     bool
	MaxCapture int
	Feedback   io.Writer
}

// ExecutionResult holds the outcome of an execution.
type ExecutionResult struct {
	Output    string
	Error     string
	ExitCode  int
	Truncated bool
}

// CommandExecutor defines the interface for running commands.
type CommandExecutor interface {
	RunCommand(ctx context.Context, parts []string, config ExecutionConfig) (ExecutionResult, error)
	RunPipeline(ctx context.Context, pipedParts [][]string, config ExecutionConfig) (ExecutionResult, error)
}

// ProcessExecutor handles running external commands and pipelines.
type ProcessExecutor struct{}

const maxScannerCapacity = 10 * 1024 * 1024

// NewProcessExecutor creates a new ProcessExecutor.
func NewProcessExecutor() *ProcessExecutor {
	return &ProcessExecutor{}
}

// RunCommand executes a single command.
func (e *ProcessExecutor) RunCommand(ctx context.Context, parts []string, config ExecutionConfig) (ExecutionResult, error) {
	if len(parts) == 0 {
		return ExecutionResult{}, fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	file, err := e.openOutputFile(config)
	if err != nil {
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file %q: %v\n", config.OutputFile, err)
		}
	}
	if file != nil {
		defer file.Close()
	}

	if err := cmd.Start(); err != nil {
		return ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("failed to start: %v", err)}, nil
	}

	var sb strings.Builder
	var mu sync.Mutex
	var wg sync.WaitGroup
	var truncated atomic.Bool
	wt := &writeTracker{feedback: config.Feedback}
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	capture := func(r io.Reader, isStderr bool) {
		defer wg.Done()

		scanner := bufio.NewScanner(r)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, maxScannerCapacity)
		var lineBuf []byte
		for scanner.Scan() {
			data := scanner.Bytes()
			// Reuse buffer and append newline for atomic write
			lineBuf = append(lineBuf[:0], data...)
			lineBuf = append(lineBuf, '\n')

			mu.Lock()
			if file != nil {
				wt.Write(file, lineBuf)
			}

			// Slice to remove the newline for other uses
			rawLine := lineBuf[:len(data)]

			var content string
			var feedbackMsg string
			if isStderr {
				feedbackMsg = fmt.Sprintf("  %s[stderr] %s%s\n", colors.ColorRed, rawLine, colors.ColorReset)
				content = fmt.Sprintf("[stderr] %s\n", rawLine)
			} else {
				feedbackMsg = fmt.Sprintf("  %s%s%s\n", colors.ColorGray, rawLine, colors.ColorReset)
				content = string(lineBuf)
			}

			if sb.Len() < maxCapture {
				remaining := maxCapture - sb.Len()
				if len(content) > remaining {
					truncated.Store(true)
				}
				content = truncateToValidUTF8(content, remaining)
				sb.WriteString(content)
			} else {
				truncated.Store(true)
			}

			if config.Feedback != nil {
				fmt.Fprint(config.Feedback, feedbackMsg)
			}
			mu.Unlock()
		}

		if err := scanner.Err(); err != nil {
			msg := fmt.Sprintf("\n[Warning] Output read error: %v", err)
			if err == bufio.ErrTooLong {
				msg = "\n[Warning] Output line too long for scanner; truncated."
			}

			mu.Lock()
			if config.Feedback != nil {
				fmt.Fprintln(config.Feedback, msg)
			}
			remaining := maxCapture - sb.Len()
			if remaining > 0 {
				if len(msg+"\n") > remaining {
					truncated.Store(true)
				}
				content := truncateToValidUTF8(msg+"\n", remaining)
				sb.WriteString(content)
			} else {
				truncated.Store(true)
			}
			mu.Unlock()
		}
	}

	wg.Add(2)
	go capture(stdout, false)
	go capture(stderr, true)
	wg.Wait()

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		exitCode = 1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return ExecutionResult{
		Output:    sb.String(),
		ExitCode:  exitCode,
		Truncated: truncated.Load(),
	}, nil
}

// RunPipeline executes a sequence of piped commands.
func (e *ProcessExecutor) RunPipeline(ctx context.Context, pipedParts [][]string, config ExecutionConfig) (ExecutionResult, error) {
	if len(pipedParts) < 2 {
		return ExecutionResult{}, fmt.Errorf("at least two commands are required for piping")
	}

	p, err := e.newPipeline(ctx, pipedParts)
	if err != nil {
		return ExecutionResult{}, err
	}
	defer p.closePipes()

	file, err := e.openOutputFile(config)
	if err != nil {
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file %q: %v\n", config.OutputFile, err)
		}
	}
	if file != nil {
		defer file.Close()
	}

	if err := p.start(); err != nil {
		p.wait() // Ensure started processes are cleaned up
		return ExecutionResult{ExitCode: 1, Error: err.Error()}, nil
	}

	stdoutStr, stderrStr, truncated := p.capture(config, file)
	exitCode, waitErr := p.wait()

	output := stdoutStr
	if stderrStr != "" {
		output = fmt.Sprintf("Output:\n%s\nErrors:\n%s", stdoutStr, stderrStr)
	}

	// Ensure exit code is non-zero if waitErr occurred
	if waitErr != nil && exitCode == 0 {
		exitCode = 1
	}

	return ExecutionResult{
		Output:    output,
		ExitCode:  exitCode,
		Truncated: truncated,
	}, nil
}

// Internal pipeline state manager
type pipeline struct {
	cmds        []*exec.Cmd
	stderrPipes []io.Reader
	stdoutPipe  io.ReadCloser
	pipes       []io.Closer
}

func (e *ProcessExecutor) newPipeline(ctx context.Context, pipedParts [][]string) (*pipeline, error) {
	p := &pipeline{cmds: make([]*exec.Cmd, len(pipedParts))}

	for i, parts := range pipedParts {
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty command at index %d", i)
		}
		p.cmds[i] = exec.CommandContext(ctx, parts[0], parts[1:]...)

		stderr, err := p.cmds[i].StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("failed to get stderr pipe for command %d: %w", i, err)
		}
		p.stderrPipes = append(p.stderrPipes, stderr)
		p.pipes = append(p.pipes, stderr)

		if i > 0 {
			p.cmds[i].Stdin = p.pipes[len(p.pipes)-2].(io.Reader)
		}

		if i < len(pipedParts)-1 {
			stdout, err := p.cmds[i].StdoutPipe()
			if err != nil {
				return nil, err
			}
			p.pipes = append(p.pipes, stdout)
		}
	}

	var err error
	p.stdoutPipe, err = p.cmds[len(p.cmds)-1].StdoutPipe()
	if err != nil {
		return nil, err
	}
	p.pipes = append(p.pipes, p.stdoutPipe)

	return p, nil
}

func (p *pipeline) start() error {
	for i, cmd := range p.cmds {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("command %d failed to start: %w", i, err)
		}
	}
	return nil
}

func (p *pipeline) capture(config ExecutionConfig, file *os.File) (string, string, bool) {
	var wg sync.WaitGroup
	var stdoutStr, stderrStr strings.Builder
	var mu sync.Mutex // Protects builders, feedback, file, writeTracker, and totalCaptured
	var truncated atomic.Bool
	wt := &writeTracker{feedback: config.Feedback}
	var totalCaptured int
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024
	}

	appendErr := func(sb *strings.Builder, err error) {
		if err == nil {
			return
		}
		msg := fmt.Sprintf("\n[Warning] Output read error: %v", err)
		if err == bufio.ErrTooLong {
			msg = "\n[Warning] Output line too long for scanner; truncated."
		}

		mu.Lock()
		defer mu.Unlock()
		if config.Feedback != nil {
			fmt.Fprintln(config.Feedback, msg)
		}
		remaining := maxCapture - totalCaptured
		if remaining > 0 {
			if len(msg+"\n") > remaining {
				truncated.Store(true)
			}
			content := truncateToValidUTF8(msg+"\n", remaining)
			sb.WriteString(content)
			totalCaptured += len(content)
		} else {
			truncated.Store(true)
		}
	}

	// Capture Stderr in parallel
	for i, r := range p.stderrPipes {
		wg.Add(1)
		go func(idx int, src io.Reader) {
			defer wg.Done()
			scanner := bufio.NewScanner(src)
			buf := make([]byte, 64*1024)
			scanner.Buffer(buf, maxScannerCapacity)
			var lineBuf []byte
			for scanner.Scan() {
				data := scanner.Bytes()
				// Reuse buffer and append newline for atomic write
				lineBuf = append(lineBuf[:0], data...)
				lineBuf = append(lineBuf, '\n')

				mu.Lock()
				if file != nil {
					wt.Write(file, lineBuf)
				}

				// Slice to remove the newline for other uses
				rawLine := lineBuf[:len(data)]

				feedbackMsg := ""
				if config.Feedback != nil {
					feedbackMsg = fmt.Sprintf("  %s[stderr:%d] %s%s\n", colors.ColorRed, idx, rawLine, colors.ColorReset)
				}

				remaining := maxCapture - totalCaptured
				if remaining > 0 {
					content := fmt.Sprintf("[stderr:%d] %s\n", idx, rawLine)
					if len(content) > remaining {
						truncated.Store(true)
					}
					content = truncateToValidUTF8(content, remaining)
					stderrStr.WriteString(content)
					totalCaptured += len(content)
				} else {
					truncated.Store(true)
				}

				if feedbackMsg != "" {
					fmt.Fprint(config.Feedback, feedbackMsg)
				}
				mu.Unlock()
			}
			appendErr(&stderrStr, scanner.Err())
		}(i, r)
	}

	// Capture Stdout sequentially (main thread)
	scanner := bufio.NewScanner(p.stdoutPipe)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)
	var lineBuf []byte
	for scanner.Scan() {
		data := scanner.Bytes()
		// Reuse buffer and append newline for atomic write
		lineBuf = append(lineBuf[:0], data...)
		lineBuf = append(lineBuf, '\n')

		mu.Lock()
		if file != nil {
			wt.Write(file, lineBuf)
		}

		// Slice to remove the newline for other uses
		rawLine := lineBuf[:len(data)]

		feedbackMsg := ""
		if config.Feedback != nil {
			feedbackMsg = fmt.Sprintf("  %s%s%s\n", colors.ColorGray, rawLine, colors.ColorReset)
		}

		remaining := maxCapture - totalCaptured
		if remaining > 0 {
			content := string(lineBuf)
			if len(content) > remaining {
				truncated.Store(true)
			}
			content = truncateToValidUTF8(content, remaining)
			stdoutStr.WriteString(content)
			totalCaptured += len(content)
		} else {
			truncated.Store(true)
		}

		if feedbackMsg != "" {
			fmt.Fprint(config.Feedback, feedbackMsg)
		}
		mu.Unlock()
	}
	appendErr(&stdoutStr, scanner.Err())

	wg.Wait()
	return stdoutStr.String(), stderrStr.String(), truncated.Load()
}

func (p *pipeline) wait() (int, error) {
	var lastErr error
	exitCode := 0
	for i := len(p.cmds) - 1; i >= 0; i-- {
		if p.cmds[i].Process == nil {
			continue
		}
		err := p.cmds[i].Wait()
		if err != nil && exitCode == 0 {
			exitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		if i == len(p.cmds)-1 {
			lastErr = err
		}
	}
	return exitCode, lastErr
}

func (p *pipeline) closePipes() {
	for _, c := range p.pipes {
		_ = c.Close()
	}
}

func (e *ProcessExecutor) openOutputFile(config ExecutionConfig) (*os.File, error) {
	if config.OutputFile == "" {
		return nil, nil
	}
	path := filepath.Clean(config.OutputFile)

	// Simple security check: prevent escaping the current directory via relative paths.
	// We allow absolute paths as the agent may need to write to specific system locations
	// if authorized, but relative paths should stay within the project structure.
	if !filepath.IsAbs(path) && (strings.HasPrefix(path, ".."+string(filepath.Separator)) || path == "..") {
		return nil, fmt.Errorf("output file path cannot escape current directory: %s", config.OutputFile)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if config.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	return os.OpenFile(path, flags, 0644)
}

// writeTracker tracks if a write to a shared output file has failed,
// ensuring only one warning is issued.
type writeTracker struct {
	failed   bool
	feedback io.Writer
}

// Write attempts to write to w. If it fails, it sets the failed flag and
// optionally sends a warning to feedback.
func (wt *writeTracker) Write(w io.Writer, p []byte) {
	if wt.failed || w == nil {
		return
	}

	// Robustness check for typed nils (e.g., *os.File(nil) passed as io.Writer)
	if f, ok := w.(*os.File); ok && f == nil {
		return
	}

	if _, err := w.Write(p); err != nil {
		wt.failed = true
		if wt.feedback != nil {
			fmt.Fprintf(wt.feedback, "\n[Warning] Write failed to output file: %v\n", err)
		}
	}
}

// truncateToValidUTF8 ensures that a string is truncated to a maximum number of bytes
// without breaking a multi-byte UTF-8 character.
func truncateToValidUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
