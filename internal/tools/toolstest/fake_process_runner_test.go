// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Compile-time assertion: FakeProcessRunner implements the tools.ProcessRunner
// port (issue #1460, ADR-074).
var _ tools.ProcessRunner = (*FakeProcessRunner)(nil)

// Compile-time assertion: fakeProcessHandle implements tools.ProcessHandle.
var _ tools.ProcessHandle = (*fakeProcessHandle)(nil)

// fakeProcessHandle is a minimal hand-rolled in-package handle double used by
// the preset-Func passthrough tests. Not exported — consumers construct their
// own; the fake's job is only to carry canned readers and a wait error.
type fakeProcessHandle struct {
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	waitErr error
}

func (h *fakeProcessHandle) Stdout() io.ReadCloser { return h.stdout }
func (h *fakeProcessHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *fakeProcessHandle) Wait() error           { return h.waitErr }

// assertStartLog verifies Start was recorded in the Calls log and is the most
// recent entry — the fake's identity assertion.
func assertStartLog(t *testing.T, f *FakeProcessRunner) {
	t.Helper()
	if !f.Called("Start") {
		t.Error("Called(\"Start\") = false; want true")
	}
	if len(f.Calls) == 0 {
		t.Fatal("Calls is empty; want last element \"Start\"")
	}
	if got := f.Calls[len(f.Calls)-1]; got != "Start" {
		t.Errorf("last Call = %q; want \"Start\"", got)
	}
}

func TestFakeProcessRunner_PresetFunc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(t *testing.T, f *FakeProcessRunner)
	}{
		{
			name: "returns the Func's handle",
			run: func(t *testing.T, f *FakeProcessRunner) {
				want := &fakeProcessHandle{
					stdout:  io.NopCloser(strings.NewReader("out")),
					stderr:  io.NopCloser(strings.NewReader("err")),
					waitErr: &tools.ExitError{Code: 3},
				}
				f.StartFunc = func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
					if spec.Name != "go" {
						t.Errorf("Start() spec.Name = %q; want %q", spec.Name, "go")
					}
					return want, nil
				}

				got, err := f.Start(ctx, tools.ProcessSpec{Name: "go", Args: []string{"version"}})
				if err != nil {
					t.Fatalf("Start() returned error: %v", err)
				}
				if got != want {
					t.Fatalf("Start() handle identity mismatch: got %p, want %p", got, want)
				}
				assertStartLog(t, f)

				// The handle carries the canned surface: readers and the
				// domain-typed exit failure round-trip through errors.As and
				// keep the os/exec wording ("exit status N").
				if s := got.Stdout(); s == nil {
					t.Error("Stdout() = nil; want non-nil")
				}
				if s := got.Stderr(); s == nil {
					t.Error("Stderr() = nil; want non-nil")
				}
				waitErr := got.Wait()
				var exitErr *tools.ExitError
				if !errors.As(waitErr, &exitErr) {
					t.Fatalf("Wait() = %v; want *tools.ExitError", waitErr)
				}
				if exitErr.Code != 3 {
					t.Errorf("ExitError.Code = %d; want 3", exitErr.Code)
				}
				if gotMsg := exitErr.Error(); gotMsg != "exit status 3" {
					t.Errorf("ExitError.Error() = %q; want %q", gotMsg, "exit status 3")
				}
			},
		},
		{
			name: "returns the Func's error",
			run: func(t *testing.T, f *FakeProcessRunner) {
				wantErr := errors.New("start failed")
				f.StartFunc = func(ctx context.Context, spec tools.ProcessSpec) (tools.ProcessHandle, error) {
					return nil, wantErr
				}

				got, err := f.Start(ctx, tools.ProcessSpec{Name: "diff"})
				if got != nil {
					t.Errorf("Start() handle = %v; want nil", got)
				}
				if !errors.Is(err, wantErr) {
					t.Errorf("Start() err = %v; want %v", err, wantErr)
				}
				assertStartLog(t, f)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &FakeProcessRunner{}
			tt.run(t, f)
		})
	}
}

func TestFakeProcessRunner_ZeroDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := &FakeProcessRunner{}
	got, err := f.Start(ctx, tools.ProcessSpec{Name: "anything"})
	if got != nil || err != nil {
		t.Errorf("Start() = (%v, %v); want (nil, nil) on unset StartFunc", got, err)
	}
	if !f.Called("Start") {
		t.Error("Start not recorded in Calls")
	}
}

func TestFakeProcessRunner_CallOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &FakeProcessRunner{}

	if _, err := f.Start(ctx, tools.ProcessSpec{Name: "a"}); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if _, err := f.Start(ctx, tools.ProcessSpec{Name: "b"}); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	want := []string{"Start", "Start"}
	if len(f.Calls) != len(want) {
		t.Fatalf("len(Calls) = %d; want %d", len(f.Calls), len(want))
	}
	for i, c := range f.Calls {
		if c != want[i] {
			t.Errorf("Calls[%d] = %q; want %q", i, c, want[i])
		}
	}
	if f.Called("Other") {
		t.Error("Called(\"Other\") = true; want false")
	}
	if !f.Called("Start") {
		t.Error("Called(\"Start\") = false; want true")
	}
}

func TestFakeProcessRunner_ConcurrentStarts(t *testing.T) {
	f := &FakeProcessRunner{}
	const goroutines = 8
	var wg sync.WaitGroup
	ctx := context.Background()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			if _, err := f.Start(ctx, tools.ProcessSpec{Name: "proc", Args: []string{"arg", string(rune('a' + seed))}}); err != nil {
				t.Errorf("Start() returned error: %v", err)
			}
		}(g)
	}
	wg.Wait()
	// All goroutines joined: reading Calls is now safe.
	if len(f.Calls) != goroutines {
		t.Errorf("len(Calls) = %d; want %d", len(f.Calls), goroutines)
	}
	if !f.Called("Start") {
		t.Error("Called(\"Start\") = false; want true after concurrent starts")
	}
}
