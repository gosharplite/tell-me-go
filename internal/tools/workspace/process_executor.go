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

	"github.com/gosharplite/tell-me-go/internal/infrastructure/encoding"
)

// executionConfig defines parameters for command or pipeline execution.
type executionConfig struct {
	OutputFile string
	Append     bool
	MaxCapture int
	Feedback   io.Writer
}

// executionResult holds the outcome of an execution.
type executionResult struct {
	Output    string
	Error     string
	ExitCode  int
	Truncated bool
}

// processExecutor handles running external commands and pipelines.
type processExecutor struct{}

const maxScannerCapacity = 10 * 1024 * 1024

// newprocessExecutor creates a new processExecutor.
func newprocessExecutor() *processExecutor {
	return &processExecutor{}
}

// RunCommand executes a single command.
func (e *processExecutor) RunCommand(ctx context.Context, parts []string, config executionConfig) (res executionResult, err error) {
	cmd, stdout, stderr, file, setupErr := e.setupCommand(ctx, parts, config)
	if setupErr != nil {
		return executionResult{ExitCode: 1}, setupErr
	}
	if file != nil {
		defer func() {
			if cerr := file.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("failed to close output file: %w", cerr)
			}
		}()
	}

	if err = cmd.Start(); err != nil {
		return executionResult{ExitCode: 1}, fmt.Errorf("failed to start: %w", err)
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

	return executionResult{
		Output:    sb.String(),
		ExitCode:  exitCode,
		Truncated: truncated.Load(),
	}, nil
}

func (e *processExecutor) prepareOutputFile(config executionConfig) *os.File {
	file, ferr := e.openOutputFile(config)
	if ferr != nil && config.Feedback != nil {
		_, _ = fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file %q: %v\n", config.OutputFile, ferr)
	}
	return file
}

func (e *processExecutor) setupCommand(ctx context.Context, parts []string, config executionConfig) (*exec.Cmd, io.ReadCloser, io.ReadCloser, *os.File, error) {
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

	return cmd, stdout, stderr, e.prepareOutputFile(config), nil
}

func (e *processExecutor) captureOutput(sb *strings.Builder, stdout, stderr io.Reader, config executionConfig, file *os.File) *atomic.Bool {
	var mu sync.Mutex
	var wg sync.WaitGroup
	truncated := &atomic.Bool{}
	wt := &writeTracker{feedback: config.Feedback, filePath: config.OutputFile}
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	wg.Add(2)
	totalCaptured := 0
	go e.captureStream(encoding.WrapReader(stdout), false, sb, &mu, &wg, truncated, wt, config, file, maxCapture, &totalCaptured)
	go e.captureStream(encoding.WrapReader(stderr), true, sb, &mu, &wg, truncated, wt, config, file, maxCapture, &totalCaptured)
	wg.Wait()

	return truncated
}

func (e *processExecutor) captureStream(r io.Reader, isStderr bool, sb *strings.Builder, mu *sync.Mutex, wg *sync.WaitGroup, truncated *atomic.Bool, wt *writeTracker, config executionConfig, file *os.File, maxCapture int, totalCaptured *int) {
	defer wg.Done()

	sp := &streamProcessor{
		mu:            mu,
		truncated:     truncated,
		totalCaptured: totalCaptured,
		wt:            wt,
		maxCapture:    maxCapture,
		feedback:      config.Feedback,
		file:          file,
	}

	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)

	prefix := ""
	if isStderr {
		prefix = "[stderr]"
	}

	for scanner.Scan() {
		sp.processLine(sb, scanner.Bytes(), prefix, config.Feedback)
	}

	e.handleCaptureError(scanner.Err(), sb, mu, config, truncated, maxCapture)
}

func (e *processExecutor) handleCaptureError(err error, sb *strings.Builder, mu *sync.Mutex, config executionConfig, truncated *atomic.Bool, maxCapture int) {
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
		_, _ = fmt.Fprintln(config.Feedback, msg)
	}

	remaining := maxCapture - sb.Len()
	if remaining > 0 {
		fullMsg := msg + "\n"
		if len(fullMsg) > remaining {
			truncated.Store(true)
		}
		content := truncateToValidUTF8(fullMsg, remaining)
		sb.WriteString(content)
	} else {
		truncated.Store(true)
	}
}

// RunPipeline executes a sequence of piped commands.
func (e *processExecutor) RunPipeline(ctx context.Context, pipedParts [][]string, config executionConfig) (res executionResult, err error) {
	if len(pipedParts) < 2 {
		return executionResult{ExitCode: 1}, fmt.Errorf("at least two commands are required for piping")
	}

	p, setupErr := e.newPipeline(ctx, pipedParts)
	if setupErr != nil {
		return executionResult{ExitCode: 1}, setupErr
	}
	defer p.closePipes()

	file := e.prepareOutputFile(config)
	if file != nil {
		defer func() {
			if cerr := file.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("failed to close output file: %w", cerr)
			}
		}()
	}

	if err = p.start(); err != nil {
		_, _ = p.wait() // Ensure started processes are cleaned up
		return executionResult{ExitCode: 1}, fmt.Errorf("pipeline failed to start: %w", err)
	}

	stdoutStr, stderrStr, truncated := p.capture(config, file)
	exitCode, waitErr := p.wait()

	return e.formatPipelineResult(stdoutStr, stderrStr, truncated, exitCode, waitErr)
}

func (e *processExecutor) formatPipelineResult(stdoutStr, stderrStr string, truncated bool, exitCode int, waitErr error) (executionResult, error) {
	output := stdoutStr
	if stderrStr != "" {
		output = fmt.Sprintf("Output:\n%s\nErrors:\n%s", stdoutStr, stderrStr)
	}

	// Ensure exit code is non-zero if waitErr occurred
	if waitErr != nil && exitCode == 0 {
		exitCode = 1
	}

	return executionResult{
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

func (e *processExecutor) newPipeline(ctx context.Context, pipedParts [][]string) (*pipeline, error) {
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

func (p *pipeline) capture(config executionConfig, file *os.File) (string, string, bool) {
	var mu sync.Mutex
	var truncated atomic.Bool
	totalCaptured := 0
	sp := &streamProcessor{
		stdoutStr:     &strings.Builder{},
		stderrStr:     &strings.Builder{},
		mu:            &mu,
		truncated:     &truncated,
		totalCaptured: &totalCaptured,
		wt:            &writeTracker{feedback: config.Feedback, filePath: config.OutputFile},
		maxCapture:    config.MaxCapture,
		feedback:      config.Feedback,
		file:          file,
	}
	if sp.maxCapture <= 0 {
		sp.maxCapture = 1024 * 1024
	}

	var wg sync.WaitGroup
	for i, r := range p.stderrPipes {
		wg.Add(1)
		go p.captureStderrAsync(&wg, sp, i, encoding.WrapReader(r))
	}

	// Capture Stdout sequentially (main thread)
	scanner := bufio.NewScanner(encoding.WrapReader(p.stdoutPipe))
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
	mu            *sync.Mutex
	truncated     *atomic.Bool
	wt            *writeTracker
	totalCaptured *int
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
		feedbackMsg = fmt.Sprintf("  %s\n", rawLine)
	}

	if prefix != "" {
		content = fmt.Sprintf("%s %s", prefix, content)
		if feedback != nil {
			feedbackMsg = fmt.Sprintf("  %s %s\n", prefix, rawLine)
		}
	}

	remaining := sp.maxCapture - *sp.totalCaptured
	if remaining > 0 {
		if len(content) > remaining {
			sp.truncated.Store(true)
		}
		content = truncateToValidUTF8(content, remaining)
		sb.WriteString(content)
		*sp.totalCaptured += len(content)
	} else {
		sp.truncated.Store(true)
	}

	if feedback != nil && feedbackMsg != "" {
		_, _ = fmt.Fprint(feedback, feedbackMsg)
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
		_, _ = fmt.Fprintln(sp.feedback, msg)
	}

	remaining := sp.maxCapture - *sp.totalCaptured
	if remaining > 0 {
		fullMsg := msg + "\n"
		if len(fullMsg) > remaining {
			sp.truncated.Store(true)
		}
		content := truncateToValidUTF8(fullMsg, remaining)
		sb.WriteString(content)
		*sp.totalCaptured += len(content)
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

func (e *processExecutor) openOutputFile(config executionConfig) (*os.File, error) {
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
				_, _ = fmt.Fprintf(wt.feedback, "\n[Warning] Failed to write to output file %q: %v\n", wt.filePath, err)
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

// Output runs the command and returns its standard output.
func (e *processExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	res, err := e.RunCommand(ctx, append([]string{name}, args...), executionConfig{})
	if err != nil {
		return []byte(res.Output), err
	}
	if res.ExitCode != 0 {
		return []byte(res.Output), fmt.Errorf("exit status %d", res.ExitCode)
	}
	return []byte(res.Output), nil
}

// CombinedOutput runs the command and returns its combined standard output and standard error.
func (e *processExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	res, err := e.RunCommand(ctx, append([]string{name}, args...), executionConfig{})
	if err != nil {
		return []byte(res.Output), err
	}
	if res.ExitCode != 0 {
		return []byte(res.Output), fmt.Errorf("exit status %d", res.ExitCode)
	}
	return []byte(res.Output), nil
}

func (e *processExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}
