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

const maxScannerCapacity = 10 * 1024 * 1024

var newline = []byte{'\n'}

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
	var writeFailed bool
	maxCapture := config.MaxCapture
	if maxCapture <= 0 {
		maxCapture = 1024 * 1024 // Default 1MB
	}

	scanner := bufio.NewScanner(multi)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxScannerCapacity)
	for scanner.Scan() {
		data := scanner.Bytes()
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "  \033[90m%s\033[0m\n", data)
		}
		if sb.Len() < maxCapture {
			remaining := maxCapture - sb.Len()
			canWrite := len(data)
			if canWrite > remaining {
				canWrite = remaining
			}
			sb.Write(data[:canWrite])
			if canWrite < remaining && sb.Len() < maxCapture {
				sb.WriteByte('\n')
			}
		}
		if file != nil && !writeFailed {
			if _, err := file.Write(data); err != nil {
				if config.Feedback != nil {
					fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file: %v\n", err)
				}
				writeFailed = true
			} else {
				file.Write(newline)
			}
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

	p, err := e.newPipeline(ctx, pipedParts)
	if err != nil {
		return ExecutionResult{}, err
	}
	defer p.closePipes()

	file, err := e.openOutputFile(config)
	if err != nil {
		return ExecutionResult{}, err
	}
	if file != nil {
		defer file.Close()
	}

	if err := p.start(); err != nil {
		p.wait() // Ensure started processes are cleaned up
		return ExecutionResult{ExitCode: 1, Error: err.Error()}, nil
	}

	stdoutStr, stderrStr := p.capture(config, file)
	exitCode, waitErr := p.wait()

	output := stdoutStr
	if stderrStr != "" {
		output = fmt.Sprintf("Output:\n%s\nErrors:\n%s", stdoutStr, stderrStr)
	}

	// Ensure exit code is non-zero if waitErr occurred
	if waitErr != nil && exitCode == 0 {
		exitCode = 1
	}

	return ExecutionResult{Output: output, ExitCode: exitCode}, nil
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
			return fmt.Errorf("command %d failed to start: %v", i, err)
		}
	}
	return nil
}

func (p *pipeline) capture(config ExecutionConfig, file *os.File) (string, string) {
	var wg sync.WaitGroup
	var stdoutStr, stderrStr strings.Builder
	var mu sync.Mutex // Protects builders, feedback, file, and writeFailed
	var writeFailed bool
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
		sb.WriteString(msg + "\n")
	}

	// Capture Stderr in parallel
	for i, r := range p.stderrPipes {
		wg.Add(1)
		go func(idx int, src io.Reader) {
			defer wg.Done()
			scanner := bufio.NewScanner(src)
			buf := make([]byte, 64*1024)
			scanner.Buffer(buf, maxScannerCapacity)
			for scanner.Scan() {
				data := scanner.Bytes()
				mu.Lock()
				if file != nil && !writeFailed {
					if _, err := file.Write(data); err != nil {
						if config.Feedback != nil {
							fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file: %v\n", err)
						}
						writeFailed = true
					} else {
						file.Write(newline)
					}
				}
				if config.Feedback != nil {
					fmt.Fprintf(config.Feedback, "  \033[31m[%d] %s\033[0m\n", idx, data)
				}
				remaining := maxCapture - stderrStr.Len()
				if remaining > 0 {
					canWrite := len(data)
					if canWrite > remaining {
						canWrite = remaining
					}
					stderrStr.Write(data[:canWrite])
					if canWrite < remaining && stderrStr.Len() < maxCapture {
						stderrStr.WriteByte('\n')
					}
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
	for scanner.Scan() {
		data := scanner.Bytes()

		mu.Lock()
		if file != nil && !writeFailed {
			if _, err := file.Write(data); err != nil {
				if config.Feedback != nil {
					fmt.Fprintf(config.Feedback, "\n[Warning] Failed to write to output file: %v\n", err)
				}
				writeFailed = true
			} else {
				file.Write(newline)
			}
		}
		if config.Feedback != nil {
			fmt.Fprintf(config.Feedback, "  \033[90m%s\033[0m\n", data)
		}
		remaining := maxCapture - stdoutStr.Len()
		if remaining > 0 {
			canWrite := len(data)
			if canWrite > remaining {
				canWrite = remaining
			}
			stdoutStr.Write(data[:canWrite])
			if canWrite < remaining && stdoutStr.Len() < maxCapture {
				stdoutStr.WriteByte('\n')
			}
		}
		mu.Unlock()
	}
	appendErr(&stdoutStr, scanner.Err())

	wg.Wait()
	return stdoutStr.String(), stderrStr.String()
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
	flags := os.O_CREATE | os.O_WRONLY
	if config.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(config.OutputFile, flags, 0644)
}
