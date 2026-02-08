// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

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
	cmd, stdout, stderr, file, err := e.setupCommand(ctx, parts, config)
	if err != nil {
		return ExecutionResult{}, err
	}
	if file != nil {
		defer file.Close()
	}

	if err := cmd.Start(); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to start: %w", err)
	}

	var sb strings.Builder
	truncated := e.captureOutput(&sb, stdout, stderr, config, file)

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

func (e *ProcessExecutor) setupCommand(ctx context.Context, parts []string, config ExecutionConfig) (*exec.Cmd, io.ReadCloser, io.ReadCloser, *os.File, error) {
	if len(parts) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	file, err := e.openOutputFile(config)
	if err != nil {
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file %q: %v\n", config.OutputFile, err)
		}
	}
	return cmd, stdout, stderr, file, nil
}

func (e *ProcessExecutor) captureOutput(sb *strings.Builder, stdout, stderr io.Reader, config ExecutionConfig, file *os.File) *atomic.Bool {
	var mu sync.Mutex
	var wg sync.WaitGroup
	truncated := &atomic.Bool{}
	wt := &writeTracker{feedback: config.Feedback, filePath: config.OutputFile}
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	wg.Add(2)
	go e.captureStream(stdout, false, sb, &mu, &wg, truncated, wt, config, file, maxCapture)
	go e.captureStream(stderr, true, sb, &mu, &wg, truncated, wt, config, file, maxCapture)
	wg.Wait()

	return truncated
}

func (e *ProcessExecutor) captureStream(r io.Reader, isStderr bool, sb *strings.Builder, mu *sync.Mutex, wg *sync.WaitGroup, truncated *atomic.Bool, wt *writeTracker, config ExecutionConfig, file *os.File, maxCapture int) {
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
		_, _ = p.wait() // Ensure started processes are cleaned up
		return ExecutionResult{}, fmt.Errorf("pipeline failed to start: %w", err)
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
	sp := &streamProcessor{
		stdoutStr:  &strings.Builder{},
		stderrStr:  &strings.Builder{},
		wt:         &writeTracker{feedback: config.Feedback, filePath: config.OutputFile},
		maxCapture: config.MaxCapture,
		feedback:   config.Feedback,
		file:       file,
	}
	if sp.maxCapture <= 0 {
		sp.maxCapture = 1024 * 1024
	}

	var wg sync.WaitGroup
	for i, r := range p.stderrPipes {
		wg.Add(1)
		go p.captureStderrAsync(&wg, sp, i, r)
	}

	// Capture Stdout sequentially (main thread)
	scanner := bufio.NewScanner(p.stdoutPipe)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)
	for scanner.Scan() {
		sp.processLine(sp.stdoutStr, scanner.Bytes(), "", sp.feedback)
	}
	sp.appendErr(sp.stdoutStr, scanner.Err())

	wg.Wait()
	return sp.stdoutStr.String(), sp.stderrStr.String(), sp.truncated.Load()
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

type streamProcessor struct {
	stdoutStr     *strings.Builder
	stderrStr     *strings.Builder
	mu            sync.Mutex
	truncated     atomic.Bool
	wt            *writeTracker
	totalCaptured int
	maxCapture    int
	feedback      io.Writer
	file          *os.File
}

func (sp *streamProcessor) processLine(sb *strings.Builder, rawLine []byte, prefix string, feedback io.Writer) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.file != nil {
		lineWithNL := append([]byte(nil), rawLine...)
		lineWithNL = append(lineWithNL, '\n')
		sp.wt.Write(sp.file, lineWithNL)
	}

	content := string(rawLine) + "\n"
	feedbackMsg := ""
	if feedback != nil {
		feedbackMsg = fmt.Sprintf("  %s%s%s\n", colors.ColorGray, rawLine, colors.ColorReset)
	}

	if prefix != "" {
		content = fmt.Sprintf("%s %s", prefix, content)
		if feedback != nil {
			feedbackMsg = fmt.Sprintf("  %s%s %s%s\n", colors.ColorRed, prefix, rawLine, colors.ColorReset)
		}
	}

	remaining := sp.maxCapture - sp.totalCaptured
	if remaining > 0 {
		if len(content) > remaining {
			sp.truncated.Store(true)
		}
		content = truncateToValidUTF8(content, remaining)
		sb.WriteString(content)
		sp.totalCaptured += len(content)
	} else {
		sp.truncated.Store(true)
	}

	if feedback != nil && feedbackMsg != "" {
		fmt.Fprint(feedback, feedbackMsg)
	}
}

func (sp *streamProcessor) appendErr(sb *strings.Builder, err error) {
	if err == nil {
		return
	}
	msg := fmt.Sprintf("\n[Warning] Output read error: %v", err)
	if err == bufio.ErrTooLong {
		msg = "\n[Warning] Output line too long for scanner; truncated."
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.feedback != nil {
		fmt.Fprintln(sp.feedback, msg)
	}

	remaining := sp.maxCapture - sp.totalCaptured
	if remaining > 0 {
		fullMsg := msg + "\n"
		if len(fullMsg) > remaining {
			sp.truncated.Store(true)
		}
		content := truncateToValidUTF8(fullMsg, remaining)
		sb.WriteString(content)
		sp.totalCaptured += len(content)
	} else {
		sp.truncated.Store(true)
	}
}

func (p *pipeline) captureStderrAsync(wg *sync.WaitGroup, sp *streamProcessor, idx int, src io.Reader) {
	defer wg.Done()
	scanner := bufio.NewScanner(src)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)
	for scanner.Scan() {
		sp.processLine(sp.stderrStr, scanner.Bytes(), fmt.Sprintf("[stderr:%d]", idx), sp.feedback)
	}
	sp.appendErr(sp.stderrStr, scanner.Err())
}

func (e *ProcessExecutor) openOutputFile(config ExecutionConfig) (*os.File, error) {
	if config.OutputFile == "" {
		return nil, nil
	}
	path := strings.TrimSpace(config.OutputFile)
	path = strings.ReplaceAll(path, "\x00", "")
	if path == "" {
		return nil, nil
	}
	path = filepath.Clean(path)

	// Robust security check: prevent escaping the current directory via relative paths.
	// We allow absolute paths as the agent may need to write to specific system locations
	// if authorized, but relative paths should stay within the project structure.
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}

		// Join CWD with the path and Clean it to resolve any ".."
		absPath := filepath.Join(cwd, path)

		// Ensure the resulting absolute path is still within the CWD
		rel, err := filepath.Rel(cwd, absPath)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return nil, fmt.Errorf("output file path cannot escape current directory: %q", config.OutputFile)
		}
		path = absPath
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
	failed   atomic.Bool
	feedback io.Writer
	filePath string
}

// Write attempts to write to w. If it fails, it sets the failed flag and
// optionally sends a warning to feedback.
func (wt *writeTracker) Write(w io.Writer, p []byte) {
	if wt.failed.Load() || w == nil {
		return
	}

	// Robustness check for typed nils (e.g., *os.File(nil) passed as io.Writer)
	if f, ok := w.(*os.File); ok && f == nil {
		return
	}

	if _, err := w.Write(p); err != nil {
		if wt.failed.CompareAndSwap(false, true) {
			if wt.feedback != nil {
				fmt.Fprintf(wt.feedback, "\n[Warning] Failed to write to output file %q: %v\n", wt.filePath, err)
			}
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
