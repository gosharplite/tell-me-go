// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/encoding"
)

// pipeline manages a sequence of piped commands. Pipe creation and wiring
// dissolve into the tools.ProcessRunner adapter (issue #1460, ADR-074):
// the executor builds ProcessSpecs, starts the stages in order wiring each
// stage's Stdin from the previous handle's Stdout, and reaps via Wait.
type pipeline struct {
	runner      tools.ProcessRunner
	specs       []tools.ProcessSpec
	handles     []tools.ProcessHandle // nil for stages not yet started
	stderrPipes []io.Reader
	stdoutPipe  io.ReadCloser
}

func (e *processExecutor) newPipeline(ctx context.Context, pipedParts [][]string, config executionConfig) (*pipeline, error) {
	p := &pipeline{
		runner:  e.runner,
		specs:   make([]tools.ProcessSpec, len(pipedParts)),
		handles: make([]tools.ProcessHandle, len(pipedParts)),
	}

	for i, parts := range pipedParts {
		// Coverage gap accepted by architect — len(parts)==0 is a defensive
		// guard on already-validated input (pipeline commands are parsed from
		// shell input and never empty at this call site).
		if len(parts) == 0 {
			return nil, fmt.Errorf("empty command at index %d", i)
		}
		p.specs[i] = tools.ProcessSpec{Name: parts[0], Args: parts[1:], Env: config.Env}
	}

	return p, nil
}

// start starts the stages in order, wiring each stage's Stdin from the
// previous handle's Stdout before starting it (ADR-074 D4 contract 1: start
// order 0..n−1, the read-end exists from Start(spec[0])'s return, and
// capture begins only after all starts — RunPipeline captures after start
// returns).
func (p *pipeline) start(ctx context.Context) error {
	for i := range p.specs {
		if i > 0 {
			// Connect the previous command's stdout to this command's stdin.
			p.specs[i].Stdin = p.handles[i-1].Stdout()
		}
		h, err := p.runner.Start(ctx, p.specs[i])
		if err != nil {
			return fmt.Errorf("command %d failed to start: %w", i, err)
		}
		p.handles[i] = h
		p.stderrPipes = append(p.stderrPipes, h.Stderr())
	}
	p.stdoutPipe = p.handles[len(p.handles)-1].Stdout()
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
	for i := len(p.handles) - 1; i >= 0; i-- {
		if p.handles[i] == nil {
			continue
		}
		err := p.handles[i].Wait()
		if err != nil && exitCode == 0 {
			exitCode = 1
			var exitErr *tools.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.Code
			}
		}
		if i == len(p.handles)-1 {
			lastErr = err
		}
	}
	return exitCode, lastErr
}

// closeHandles closes every started handle's handed-out readers. This
// covers the read-ends the old closePipes list held: all stderr read-ends,
// the intermediate stdout read-ends (wired as later stages' Stdin — the
// same objects, closed via the owning handle's Stdout()), and the final
// stdout read-end. Post-Wait double-closes are tolerated and their errors
// ignored (ADR-074 D4 contract 3 — cmd.Wait already closes the pipes).
func (p *pipeline) closeHandles() {
	for _, h := range p.handles {
		if h == nil {
			continue
		}
		_ = h.Stdout().Close()
		_ = h.Stderr().Close()
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
