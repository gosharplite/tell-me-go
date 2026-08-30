// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

// fakeProcessHandle is a minimal hand-rolled in-package handle double for the
// wiring probe (unexported; the toolstest fake's handle is package-private,
// so this test carries its own canned type — T6 may duplicate or promote it
// in-package per ADR-074 D6).
type fakeProcessHandle struct {
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	waitErr error
}

func (h *fakeProcessHandle) Stdout() io.ReadCloser { return h.stdout }
func (h *fakeProcessHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *fakeProcessHandle) Wait() error           { return h.waitErr }

// fakeHandle builds a canned fakeProcessHandle: fixed stdout/stderr content
// and a Wait result (nil waitErr → Wait returns nil). Shared by the wiring
// probe and the fault-path suites.
func fakeHandle(stdout, stderr string, waitErr error) *fakeProcessHandle {
	return &fakeProcessHandle{
		stdout:  io.NopCloser(strings.NewReader(stdout)),
		stderr:  io.NopCloser(strings.NewReader(stderr)),
		waitErr: waitErr,
	}
}

// TestRunnerWiring_RunCommandReachesFakeRunner is the workspace behavioral
// probe (issue #1460, ADR-074, ADR-060 §7 rationale): a dropped
// `runner: runner` assignment in newshellTool or a dropped `e.runner.Start`
// call in RunCommand compiles (nil satisfies the interface), so the
// call-reaches-fake assertion is the enforcement — not review.
func TestRunnerWiring_RunCommandReachesFakeRunner(t *testing.T) {
	t.Parallel()

	var capturedSpec tools.ProcessSpec
	fake := &toolstest.FakeProcessRunner{
		StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
			capturedSpec = spec
			return &fakeProcessHandle{
				stdout:  io.NopCloser(strings.NewReader("wired\n")),
				stderr:  io.NopCloser(strings.NewReader("")),
				waitErr: nil,
			}, nil
		},
	}
	e := newprocessExecutorWithFS(infra_persistence.NewOSFileSystem(), fake)

	res, err := e.RunCommand(context.Background(), []string{"any", "cmd"}, executionConfig{})
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}

	// (a) The fake was reached: newshellTool → executor.runner and
	// RunCommand → runner.Start are both proven by this single assertion.
	if !fake.Called("Start") {
		t.Fatal("Called(\"Start\") = false — RunCommand did not reach the injected runner")
	}

	// (b) The result carries the canned handle's output with a clean exit.
	if !strings.Contains(res.Output, "wired\n") {
		t.Errorf("RunCommand output = %q; want it to contain %q", res.Output, "wired\n")
	}
	if res.ExitCode != 0 {
		t.Errorf("RunCommand ExitCode = %d; want 0", res.ExitCode)
	}

	// (c) The spec-assembly contract: name and args flow from RunCommand's
	// parts into the port's ProcessSpec.
	if capturedSpec.Name != "any" {
		t.Errorf("captured spec.Name = %q; want %q", capturedSpec.Name, "any")
	}
	if len(capturedSpec.Args) != 1 || capturedSpec.Args[0] != "cmd" {
		t.Errorf("captured spec.Args = %v; want [cmd]", capturedSpec.Args)
	}
	if capturedSpec.Stdin != nil {
		t.Errorf("captured spec.Stdin = %v; want nil (RunCommand sets no stdin)", capturedSpec.Stdin)
	}
	if capturedSpec.Env != nil {
		t.Errorf("captured spec.Env = %v; want nil (empty executionConfig.Env → pure inherit)", capturedSpec.Env)
	}
}
