// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var _ tools.ProcessRunner = (*runner)(nil)

// environ is a test hook for os.Environ (ADR-074 Decision 4, contract 4):
// tests override it to drive the overlay assembly deterministically. The
// default is os.Environ, preserving today's behavior.
var environ = os.Environ

// runner is the os/exec-backed implementation of tools.ProcessRunner
// (issue #1460, ADR-074). It owns the process lifecycle: exec.Cmd
// construction, the platform process-group cancellation contract
// (proc_posix.go / proc_windows.go), pipe wiring, and the exit-signal
// conversion of ADR-074 Decision 3.
type runner struct{}

// NewRunner returns the production tools.ProcessRunner implementation. The
// single construction site is internal/infrastructure/di/process_factory.go.
func NewRunner() tools.ProcessRunner {
	return &runner{}
}

// applyEnv assembles cmd.Env per ADR-074 Decision 4, contract 4: an empty
// (or nil) overlay leaves cmd.Env nil — pure inherit; a non-nil overlay is
// os.Environ() + appended k=v entries (os/exec's documented dedup-keeps-last
// consumes the appended bindings). Extracted so tests can drive the
// assembly through the environ hook without spawning a process.
func applyEnv(cmd *exec.Cmd, env map[string]string) {
	if len(env) == 0 {
		return
	}
	base := environ()
	for k, v := range env {
		base = append(base, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = base
}

// Start assembles and starts the process, behavior-preserving per
// ADR-074 Decision 4: pipes are created BEFORE cmd.Start (so pipe-creation
// failures are structurally unreachable and the handle getters carry no
// error returns), the process-group cancellation contract is applied via
// configureProcAttrs, and WaitDelay keeps today's 2s constant.
func (r *runner) Start(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
	cmd := exec.CommandContext(ctx, spec.Name, spec.Args...)
	configureProcAttrs(cmd)         // relocated proc files own the tree-kill contract
	cmd.WaitDelay = 2 * time.Second // today's process_executor.go:141 constant
	applyEnv(cmd, spec.Env)         // ADR-074 D4 contract 4 — assembly lives here

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		// Structurally unreachable: StdoutPipe fails only after Start or on a
		// second call (INTENTIONAL_NON_FIXES.md, structurally-unreachable).
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}

	// *os.File read-ends → os/exec fd fast path (direct inheritance, no copy
	// goroutine); foreign io.Readers get os/exec's documented copy semantics.
	// The asymmetry is os/exec's own dispatch on the concrete type
	// (ADR-074 D4 contract 2).
	cmd.Stdin = spec.Stdin

	if err := cmd.Start(); err != nil {
		// Start-failure path: close the handed-out readers (ADR-074 D4
		// contract 3). Raw errors — wrapping is the consumer's job.
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	return &handle{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

// handle is the running-process view of a started command. It implements
// tools.ProcessHandle.
type handle struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
}

// Stdout returns the read-end created before Start; valid only after the
// owning Start returns (ADR-074 D2 — no error return: pipe creation is
// structurally unreachable-failure at this point).
func (h *handle) Stdout() io.ReadCloser { return h.stdout }

// Stderr returns the read-end created before Start; valid only after the
// owning Start returns (ADR-074 D2 — no error return).
func (h *handle) Stderr() io.ReadCloser { return h.stderr }

// Wait reaps the process, converts exit-status failures per ADR-074
// Decision 3 (any *exec.ExitError → *tools.ExitError; others pass through),
// and closes the handed-out readers — cmd.Wait already closes them, so the
// double-close is tolerated and its errors ignored (ADR-074 D4 contract 3).
func (h *handle) Wait() error {
	err := h.cmd.Wait()
	_ = h.stdout.Close()
	_ = h.stderr.Close()
	return toExitError(err)
}

// toExitError converts any *exec.ExitError into the domain-typed
// *tools.ExitError — NOT Exited()-gated: a signal-killed child yields
// ExitCode() == -1, which becomes Code: -1 (today's convention, made
// fake-constructible). Non-ExitError failures pass through unconverted
// with identity preserved.
func toExitError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &tools.ExitError{Code: exitErr.ExitCode()}
	}
	return err
}
