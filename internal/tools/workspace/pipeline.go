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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/encoding"
)

// pipeline manages a sequence of piped commands.
type pipeline struct {
	cmds        []*exec.Cmd
	stderrPipes []io.Reader
	stdoutPipe  io.ReadCloser
	pipes       []io.Closer
}

func (e *processExecutor) newPipeline(ctx context.Context, pipedParts [][]string, config executionConfig) (*pipeline, error) {
	p := &pipeline{cmds: make([]*exec.Cmd, len(pipedParts))}

	for i, parts := range pipedParts {
		cmd, err := e.newPipelineCmd(ctx, parts, i, config)
		if err != nil {
			return nil, err
		}
		p.cmds[i] = cmd
	}

	wp := p.wirePipes
	if e.wirePipesFn != nil {
		wp = func() error { return e.wirePipesFn(p) }
	}
	if err := wp(); err != nil {
		return nil, err
	}

	return p, nil
}

// newPipelineCmd creates and configures a single exec.Cmd for use in a pipeline.
func (e *processExecutor) newPipelineCmd(ctx context.Context, parts []string, index int, config executionConfig) (*exec.Cmd, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command at index %d", index)
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	if runtime.GOOS == "windows" {
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		}
	}

	if len(config.Env) > 0 {
		env := os.Environ()
		for k, v := range config.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	return cmd, nil
}

// wirePipes connects stderr, stdin, and stdout pipes across all commands in the pipeline.
func (p *pipeline) wirePipes() error {
	var piped bool
	defer func() {
		if !piped {
			p.closePipes()
		}
	}()

	for i, cmd := range p.cmds {
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to get stderr pipe for command %d: %w", i, err)
		}
		p.stderrPipes = append(p.stderrPipes, stderr)
		p.pipes = append(p.pipes, stderr)

		if i > 0 {
			// Connect previous command's stdout to this command's stdin.
			p.cmds[i].Stdin = p.pipes[len(p.pipes)-2].(io.Reader)
		}

		if i < len(p.cmds)-1 {
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return fmt.Errorf("failed to get stdout pipe for command %d: %w", i, err)
			}
			p.pipes = append(p.pipes, stdout)
		}
	}

	var err error
	p.stdoutPipe, err = p.cmds[len(p.cmds)-1].StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe for last command: %w", err)
	}
	p.pipes = append(p.pipes, p.stdoutPipe)

	piped = true // success — caller is now responsible for closePipes()
	return nil
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
