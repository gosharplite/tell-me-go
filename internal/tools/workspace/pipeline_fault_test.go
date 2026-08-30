// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

// TestPipelineWaitFaultSemantics pins the pipeline wait semantics 1:1 through
// per-stage fake handles over three stages (reverse-loop ordering, lastErr
// from the last command, formatPipelineResult's !ok rule) — issue #1460,
// ADR-074. These are the branches the #1431 batch cataloged as
// fault-injection-required in pipeline wait/capture.
func TestPipelineWaitFaultSemantics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// runFaultPipeline starts three stages (stage0/1/2); waitErrs maps a
	// stage name to its handle's Wait result; errStderr names a stage whose
	// Stderr reader errors (capture warning path).
	runFaultPipeline := func(t *testing.T, waitErrs map[string]error, errStderr string) (executionResult, error) {
		t.Helper()
		fake := &toolstest.FakeProcessRunner{
			StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
				h := fakeHandle(spec.Name+"-out\n", "", waitErrs[spec.Name])
				if spec.Name == errStderr {
					h.stderr = io.NopCloser(iotest.ErrReader(errors.New("stderr boom")))
				}
				return h, nil
			},
		}
		e := newFaultExecutor(fake)
		return e.RunPipeline(ctx, [][]string{
			{stageName(0)}, {stageName(1)}, {stageName(2)},
		}, executionConfig{})
	}

	t.Run("last-stage exit 3 surfaces with nil error", func(t *testing.T) {
		t.Parallel()
		res, err := runFaultPipeline(t, map[string]error{
			stageName(2): &tools.ExitError{Code: 3},
		}, "")
		if err != nil {
			t.Fatalf("err = %v; want nil (exit-status failure)", err)
		}
		if res.ExitCode != 3 {
			t.Errorf("ExitCode = %d; want 3", res.ExitCode)
		}
	})

	t.Run("non-last-stage-only exit 7 surfaces with nil error and clean lastErr", func(t *testing.T) {
		t.Parallel()
		res, err := runFaultPipeline(t, map[string]error{
			stageName(0): &tools.ExitError{Code: 7},
		}, "")
		if err != nil {
			t.Fatalf("err = %v; want nil (last command succeeded → lastErr nil)", err)
		}
		if res.ExitCode != 7 {
			t.Errorf("ExitCode = %d; want 7", res.ExitCode)
		}
	})

	t.Run("last-stage non-exit error propagates with exit 1", func(t *testing.T) {
		t.Parallel()
		res, err := runFaultPipeline(t, map[string]error{
			stageName(2): errors.New("boom-last"),
		}, "")
		if err == nil || !strings.Contains(err.Error(), "boom-last") {
			t.Fatalf("err = %v; want the unconverted last-command error", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("ExitCode = %d; want 1", res.ExitCode)
		}
	})

	t.Run("mixed failures take the reverse-order first error code", func(t *testing.T) {
		t.Parallel()
		// Reverse wait order: stage1's plain error is seen BEFORE stage0's
		// exit 7, so the exit code is 1 — and the succeeding last command
		// leaves lastErr nil → formatPipelineResult returns a nil error.
		res, err := runFaultPipeline(t, map[string]error{
			stageName(0): &tools.ExitError{Code: 7},
			stageName(1): errors.New("mid-fail"),
		}, "")
		if err != nil {
			t.Fatalf("err = %v; want nil (last command succeeded)", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("ExitCode = %d; want 1 (stage1's plain error wins the reverse walk)", res.ExitCode)
		}
	})

	t.Run("erroring stderr reader warns while stdout stays intact", func(t *testing.T) {
		t.Parallel()
		res, err := runFaultPipeline(t, map[string]error{}, stageName(1))
		if err != nil {
			t.Fatalf("err = %v; want nil", err)
		}
		if !strings.Contains(res.Output, "stage2-out") {
			t.Errorf("Output = %q; want the last stage's stdout intact", res.Output)
		}
		if !strings.Contains(res.Output, "[Warning] Output read error") {
			t.Errorf("Output = %q; want the stderr capture warning", res.Output)
		}
		if res.ExitCode != 0 {
			t.Errorf("ExitCode = %d; want 0", res.ExitCode)
		}
	})
}
