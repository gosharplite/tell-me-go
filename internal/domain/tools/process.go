// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"io"
)

// ProcessRunner starts external processes. Implemented by
// internal/infrastructure/process; consumed by internal/tools/workspace
// (issue #1460, ADR-074).
//
// Zero dead members (ADR-074 Decision 2, ADR-003 Rule #2): the port carries
// no Dir on ProcessSpec (no consumer sets cmd.Dir), no LookPath on the
// runner (the pwsh probe is shell-translation logic; processExecutor.LookPath
// remains a direct method), no Close on ProcessHandle (reader-level
// ownership plus Wait-based reaping reproduces today's cleanup), and no
// StartPipeline (exactly one pipeline consumer; the wire/start interleave
// is pinned behavior-preserving). Every cut is consumer-traced in
// ADR-074 Decision 2.
type ProcessRunner interface {
	Start(ctx context.Context, spec ProcessSpec) (ProcessHandle, error)
}

// ProcessSpec is the start request for ProcessRunner.Start.
//
// Stdin nil = no stdin (today's behavior). Env nil = inherit os.Environ()
// untouched; a non-nil map is an overlay (os.Environ() + append, last-wins
// per key) assembled by the adapter (ADR-074 Decision 4, contract 4).
type ProcessSpec struct {
	Name  string
	Args  []string
	Stdin io.Reader         // nil = no stdin (today's behavior)
	Env   map[string]string // overlay map; nil = inherit os.Environ() untouched
}

// ProcessHandle is the running-process view returned by ProcessRunner.Start.
//
// Validity window (ADR-074 Decision 4, contract 1): Stdout and Stderr are
// valid only after the owning Start returns; the adapter creates the pipes
// before cmd.Start, so the getters carry no error returns — pipe-creation
// failures are structurally unreachable at that point in the lifecycle.
//
// Wait returns the process result: exit-status failures come back as
// *ExitError (any *exec.ExitError from the adapter's Wait converts,
// including signal kills — ADR-074 Decision 3); every other wait failure
// passes through unconverted.
type ProcessHandle interface {
	Stdout() io.ReadCloser // valid only after the owning Start returns
	Stderr() io.ReadCloser // valid only after the owning Start returns
	Wait() error           // exit-status failures return *ExitError
}

// ExitError is the domain-typed exit-status failure of ProcessHandle.Wait.
// Code -1 = signal-killed (ADR-074 Decision 3 — any *exec.ExitError from
// Wait() converts, preserving today's -1 convention for signals).
type ExitError struct{ Code int }

// Error mirrors the os/exec wording ("exit status N") so log output and
// tool results keep their current shape (ADR-074 Decision 3).
func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }
