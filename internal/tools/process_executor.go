// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	Output   string
	Error    string
	ExitCode int
}

// ProcessExecutor handles running external commands and pipelines.
type ProcessExecutor struct{}

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

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	multi := io.MultiReader(stdout, stderr)

	file, err := e.openOutputFile(config)
	if err != nil {
		return ExecutionResult{}, err
	}
	if file != nil {
		defer file.Close()
	}

	if err := cmd.Start(); err != nil {
		return ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("failed to start: %v", err)}, nil
	}

	var sb strings.Builder
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	scanner := bufio.NewScanner(multi)
	for scanner.Scan() {
		line := scanner.Text()
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "  \033[90m%s\033[0m\n", line)
		}
		if sb.Len() < maxCapture {
			sb.WriteString(line + "\n")
		}
		if file != nil {
			file.WriteString(line + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		msg := fmt.Sprintf("\n[Warning] Output read error: %v", err)
		if err == bufio.ErrTooLong {
			msg = "\n[Warning] Output line too long for scanner; truncated."
		}
		if config.Feedback != nil {
			fmt.Fprintln(config.Feedback, msg)
		}
		sb.WriteString(msg + "\n")
	}

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		exitCode = 1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return ExecutionResult{
		Output:   sb.String(),
		ExitCode: exitCode,
	}, nil
}

// RunPipeline executes a sequence of piped commands.
func (e *ProcessExecutor) RunPipeline(ctx context.Context, pipedParts [][]string, config ExecutionConfig) (ExecutionResult, error) {
	if len(pipedParts) < 2 {
		return ExecutionResult{}, fmt.Errorf("at least two commands are required for piping")
	}

	cmds := make([]*exec.Cmd, len(pipedParts))
	var combinedStderr strings.Builder
	var stderrPipes []io.Reader

	for i, parts := range pipedParts {
		if len(parts) == 0 {
			return ExecutionResult{}, fmt.Errorf("empty command at index %d", i)
		}
		cmds[i] = exec.CommandContext(ctx, parts[0], parts[1:]...)
		se, _ := cmds[i].StderrPipe()
		stderrPipes = append(stderrPipes, se)
	}

	// Track pipes for cleanup
	var pipes []io.Closer
	for _, se := range stderrPipes {
		pipes = append(pipes, se.(io.Closer))
	}
	defer func() {
		for _, p := range pipes {
			_ = p.Close()
		}
	}()

	// Setup stdout/stdin pipes
	for i := 0; i < len(cmds)-1; i++ {
		pipe, err := cmds[i].StdoutPipe()
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to create pipe for command %d: %w", i, err)
		}
		pipes = append(pipes, pipe)
		cmds[i+1].Stdin = pipe
	}

	lastCmd := cmds[len(cmds)-1]
	stdout, _ := lastCmd.StdoutPipe()
	pipes = append(pipes, stdout)

	file, err := e.openOutputFile(config)
	if err != nil {
		return ExecutionResult{}, err
	}
	if file != nil {
		defer file.Close()
	}

	// Start all commands
	for i := 0; i < len(cmds); i++ {
		if err := cmds[i].Start(); err != nil {
			return ExecutionResult{
				ExitCode: 1,
				Error:    fmt.Sprintf("Command %d failed to start: %v", i, err),
			}, nil
		}
	}

	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	// Read all stderr pipes in parallel
	var wg sync.WaitGroup
	var stderrMu sync.Mutex
	for i, se := range stderrPipes {
		wg.Add(1)
		go func(idx int, r io.Reader) {
			defer wg.Done()
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()
				stderrMu.Lock()
				if config.Feedback != nil {
					fmt.Fprintf(config.Feedback, "  \033[31m[%d] %s\033[0m\n", idx, line)
				}
				if combinedStderr.Len() < maxCapture {
					combinedStderr.WriteString(line + "\n")
				}
				stderrMu.Unlock()
			}
		}(i, se)
	}

	var sb strings.Builder
	stdoutScanner := bufio.NewScanner(stdout)
	for stdoutScanner.Scan() {
		line := stdoutScanner.Text()
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "  \033[90m%s\033[0m\n", line)
		}
		if sb.Len() < maxCapture {
			sb.WriteString(line + "\n")
		}
		if file != nil {
			file.WriteString(line + "\n")
		}
	}

	wg.Wait()
	pipes = nil // Clear so deferred Close() don't interfere with Wait()

	var lastErr error
	exitCode := 0
	for i := len(cmds) - 1; i >= 0; i-- {
		err := cmds[i].Wait()
		if err != nil && i == len(cmds)-1 {
			lastErr = err
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}
	}

	output := sb.String()
	errStr := combinedStderr.String()

	result := output
	if errStr != "" {
		result = fmt.Sprintf("Output:\n%s\nErrors:\n%s", output, errStr)
	}

	if lastErr != nil && exitCode == 0 {
		exitCode = 1
	}

	return ExecutionResult{
		Output:   result,
		ExitCode: exitCode,
	}, nil
}

func (e *ProcessExecutor) openOutputFile(config ExecutionConfig) (*os.File, error) {
	if config.OutputFile == "" {
		return nil, nil
	}
	flags := os.O_CREATE | os.O_WRONLY
	if config.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(config.OutputFile, flags, 0644)
}
