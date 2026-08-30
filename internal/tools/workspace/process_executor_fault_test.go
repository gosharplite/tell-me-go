// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

// newFaultExecutor builds a processExecutor over the real filesystem adapter
// and the supplied fake runner (fs is unused for executionConfig{} paths).
func newFaultExecutor(fake *toolstest.FakeProcessRunner) *processExecutor {
	return newprocessExecutorWithFS(infra_persistence.NewOSFileSystem(), fake)
}

// TestRunCommandFaultPaths pins every RunCommand branch that the #1431 batch
// cataloged as fault-injection-required, now deterministic behind the
// tools.ProcessRunner fake (issue #1460, ADR-074 D3/D4 contracts).
func TestRunCommandFaultPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name            string
		startErr        error  // StartFunc error
		waitErr         error  // handle.Wait error
		stdout          string // canned stdout
		stderr          string // canned stderr
		errReaderStdout bool   // Stdout() returns an erroring reader
		precancel       bool   // cancel ctx before RunCommand
		verify          func(t *testing.T, res executionResult, err error, fake *toolstest.FakeProcessRunner)
	}{
		{
			name:     "start error wraps and reports exit 1",
			startErr: errors.New("boom"),
			verify: func(t *testing.T, res executionResult, err error, fake *toolstest.FakeProcessRunner) {
				t.Helper()
				if err == nil || !strings.Contains(err.Error(), "failed to start: boom") {
					t.Errorf("err = %v; want wrapped 'failed to start: boom'", err)
				}
				if res.ExitCode != 1 {
					t.Errorf("ExitCode = %d; want 1", res.ExitCode)
				}
				if len(fake.Calls) != 1 {
					t.Errorf("Start invocations = %d; want 1 (fake not re-invoked)", len(fake.Calls))
				}
			},
		},
		{
			name:    "exit code 3 surfaces with nil error",
			waitErr: &tools.ExitError{Code: 3},
			stdout:  "out\n",
			verify: func(t *testing.T, res executionResult, err error, _ *toolstest.FakeProcessRunner) {
				t.Helper()
				if err != nil {
					t.Errorf("err = %v; want nil for exit-status failure", err)
				}
				if res.ExitCode != 3 {
					t.Errorf("ExitCode = %d; want 3", res.ExitCode)
				}
				if !strings.Contains(res.Output, "out") {
					t.Errorf("Output = %q; want canned stdout preserved", res.Output)
				}
			},
		},
		{
			name:    "signal-killed child surfaces code -1 with nil error",
			waitErr: &tools.ExitError{Code: -1},
			stdout:  "out\n",
			verify: func(t *testing.T, res executionResult, err error, _ *toolstest.FakeProcessRunner) {
				t.Helper()
				if err != nil {
					t.Errorf("err = %v; want nil for the -1 signal convention", err)
				}
				if res.ExitCode != -1 {
					t.Errorf("ExitCode = %d; want -1", res.ExitCode)
				}
			},
		},
		{
			name:    "non-exit wait error passes through with exit 1",
			waitErr: errors.New("wait failed"),
			stdout:  "partial\n",
			verify: func(t *testing.T, res executionResult, err error, _ *toolstest.FakeProcessRunner) {
				t.Helper()
				if err == nil || err.Error() != "wait failed" {
					t.Errorf("err = %v; want the unconverted wait error", err)
				}
				if res.ExitCode != 1 {
					t.Errorf("ExitCode = %d; want 1", res.ExitCode)
				}
			},
		},
		{
			name:      "pre-cancelled context returns partial output and ctx error",
			stdout:    "partial\n",
			precancel: true,
			verify: func(t *testing.T, res executionResult, err error, _ *toolstest.FakeProcessRunner) {
				t.Helper()
				if !errors.Is(err, context.Canceled) {
					t.Errorf("err = %v; want context.Canceled", err)
				}
				if !strings.Contains(res.Output, "partial") {
					t.Errorf("Output = %q; want partial output preserved", res.Output)
				}
				if res.ExitCode != 1 {
					t.Errorf("ExitCode = %d; want 1", res.ExitCode)
				}
			},
		},
		{
			name:            "erroring stdout reader hits the capture warning path without panicking",
			errReaderStdout: true,
			verify: func(t *testing.T, res executionResult, err error, _ *toolstest.FakeProcessRunner) {
				t.Helper()
				if err != nil {
					t.Fatalf("err = %v; want nil (capture warning is not fatal)", err)
				}
				if !strings.Contains(res.Output, "[Warning] Output read error") {
					t.Errorf("Output = %q; want the capture-scanner warning", res.Output)
				}
			},
		},
		{
			name:   "happy path combines stdout and stderr with exit 0",
			stdout: "hello\n",
			stderr: "oops\n",
			verify: func(t *testing.T, res executionResult, err error, _ *toolstest.FakeProcessRunner) {
				t.Helper()
				if err != nil {
					t.Fatalf("err = %v; want nil", err)
				}
				if !strings.Contains(res.Output, "hello") {
					t.Errorf("Output = %q; want stdout content", res.Output)
				}
				if !strings.Contains(res.Output, "[stderr] oops") {
					t.Errorf("Output = %q; want prefixed stderr content", res.Output)
				}
				if res.ExitCode != 0 {
					t.Errorf("ExitCode = %d; want 0", res.ExitCode)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &toolstest.FakeProcessRunner{
				StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
					if tt.startErr != nil {
						return nil, tt.startErr
					}
					if tt.errReaderStdout {
						return &fakeProcessHandle{
							stdout: io.NopCloser(iotest.ErrReader(errors.New("read boom"))),
							stderr: io.NopCloser(strings.NewReader("")),
						}, nil
					}
					return fakeHandle(tt.stdout, tt.stderr, tt.waitErr), nil
				},
			}
			e := newFaultExecutor(fake)

			runCtx := ctx
			if tt.precancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				defer cancel()
				runCtx = canceled
			}

			res, err := e.RunCommand(runCtx, []string{"any", "cmd"}, executionConfig{})
			tt.verify(t, res, err, fake)
		})
	}
}

// TestOutputCombinedOutputFaultPaths pins Output/CombinedOutput over the port:
// exit 0 → bytes, exit-status failure → "exit status N" error + bytes,
// start failure → raw error passthrough.
func TestOutputCombinedOutputFaultPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		method   string // "Output" or "CombinedOutput"
		startErr error
		waitErr  error
		stdout   string
		wantErr  string // empty = nil error expected
	}{
		{
			name:   "Output exit 0 returns bytes",
			method: "Output",
			stdout: "out\n",
		},
		{
			name:    "Output exit-status failure returns exit status error with bytes",
			method:  "Output",
			waitErr: &tools.ExitError{Code: 2},
			stdout:  "out\n",
			wantErr: "exit status 2",
		},
		{
			name:     "Output start error passes through",
			method:   "Output",
			startErr: errors.New("spawn fail"),
			wantErr:  "failed to start: spawn fail",
		},
		{
			name:   "CombinedOutput exit 0 returns bytes",
			method: "CombinedOutput",
			stdout: "combined\n",
		},
		{
			name:    "CombinedOutput exit-status failure returns exit status error with bytes",
			method:  "CombinedOutput",
			waitErr: &tools.ExitError{Code: 2},
			stdout:  "combined\n",
			wantErr: "exit status 2",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &toolstest.FakeProcessRunner{
				StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
					if tt.startErr != nil {
						return nil, tt.startErr
					}
					return fakeHandle(tt.stdout, "", tt.waitErr), nil
				},
			}
			e := newFaultExecutor(fake)

			var got []byte
			var err error
			if tt.method == "Output" {
				got, err = e.Output(ctx, "any", "cmd")
			} else {
				got, err = e.CombinedOutput(ctx, "any", "cmd")
			}

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("%s() err = %v; want nil", tt.method, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("%s() err = %v; want %q", tt.method, err, tt.wantErr)
				}
			}
			if tt.startErr == nil && !strings.Contains(string(got), strings.TrimSuffix(tt.stdout, "\n")) {
				t.Errorf("%s() bytes = %q; want canned stdout", got, tt.stdout)
			}
		})
	}
}

// TestRunPipelineFaultCleanup pins RunPipeline's fault paths: start-error
// cleanup with no further starts, the arity guard, and the spec-assembly
// guard — all deterministic through the fake runner.
func TestRunPipelineFaultCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("start error on stage k cleans up without further starts", func(t *testing.T) {
		t.Parallel()
		for _, failAt := range []int{0, 1} {
			failAt := failAt
			t.Run("fail at stage "+strconv.Itoa(failAt), func(t *testing.T) {
				t.Parallel()

				fake := &toolstest.FakeProcessRunner{
					StartFunc: func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
						if spec.Name == stageName(failAt) {
							return nil, errors.New("spawn fail")
						}
						return fakeHandle("", "", nil), nil
					},
				}
				e := newFaultExecutor(fake)

				res, err := e.RunPipeline(ctx, [][]string{
					{stageName(0)}, {stageName(1)}, {stageName(2)},
				}, executionConfig{})
				if err == nil || !strings.Contains(err.Error(), "pipeline failed to start") {
					t.Fatalf("err = %v; want 'pipeline failed to start' wrapping", err)
				}
				want := "command " + strconv.Itoa(failAt) + " failed to start"
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v; want inner %q", err, want)
				}
				if res.ExitCode != 1 {
					t.Errorf("ExitCode = %d; want 1", res.ExitCode)
				}
				// Early-exit reaping ran (closeHandles + wait over started
				// handles) and stages after the failure never started: the
				// call log holds exactly failAt successes + the failing
				// attempt.
				if len(fake.Calls) != failAt+1 {
					t.Errorf("Start invocations = %d; want %d", len(fake.Calls), failAt+1)
				}
			})
		}
	})

	t.Run("arity guard rejects a single command without runner involvement", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{}
		e := newFaultExecutor(fake)

		res, err := e.RunPipeline(ctx, [][]string{{"a"}}, executionConfig{})
		if err == nil || !strings.Contains(err.Error(), "at least two commands are required") {
			t.Fatalf("err = %v; want the arity error", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("ExitCode = %d; want 1", res.ExitCode)
		}
		if len(fake.Calls) != 0 {
			t.Errorf("Start invocations = %d; want 0", len(fake.Calls))
		}
	})

	t.Run("empty parts at index 2 propagates with zero starts", func(t *testing.T) {
		t.Parallel()
		fake := &toolstest.FakeProcessRunner{}
		e := newFaultExecutor(fake)

		res, err := e.RunPipeline(ctx, [][]string{{"a"}, {"b"}, {}}, executionConfig{})
		if err == nil || !strings.Contains(err.Error(), "empty command at index 2") {
			t.Fatalf("err = %v; want the spec-assembly guard error", err)
		}
		if res.ExitCode != 1 {
			t.Errorf("ExitCode = %d; want 1", res.ExitCode)
		}
		if len(fake.Calls) != 0 {
			t.Errorf("Start invocations = %d; want 0", len(fake.Calls))
		}
	})
}

func stageName(i int) string { return "stage" + strconv.Itoa(i) }
