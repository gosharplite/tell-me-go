// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package process

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ① env contract — unit level: nil/empty overlay leaves cmd.Env nil
// (pure inherit); a non-nil overlay is environ() output + appended k=v
// entries (os/exec's dedup-keeps-last consumes the appended bindings).
func TestApplyEnv_NilOrEmptyEnvLeavesCmdEnvNil(t *testing.T) {
	t.Parallel()

	var cmd exec.Cmd
	applyEnv(&cmd, nil)
	if cmd.Env != nil {
		t.Errorf("applyEnv(nil) set cmd.Env = %v; want nil (pure inherit)", cmd.Env)
	}

	applyEnv(&cmd, map[string]string{})
	if cmd.Env != nil {
		t.Errorf("applyEnv(empty) set cmd.Env = %v; want nil (pure inherit)", cmd.Env)
	}
}

func TestApplyEnv_OverlayAppendsLast(t *testing.T) {
	// Not parallel: overrides the package-level environ hook.
	orig := environ
	defer func() { environ = orig }()
	environ = func() []string { return []string{"A=1", "PATH=/bin:/usr/bin"} }

	var cmd exec.Cmd
	overlay := map[string]string{"TMGO_PROBE": "winner", "A": "override"}
	applyEnv(&cmd, overlay)

	wantBase := []string{"A=1", "PATH=/bin:/usr/bin"}
	if len(cmd.Env) != len(wantBase)+len(overlay) {
		t.Fatalf("cmd.Env = %v; want %d inherited + %d appended", cmd.Env, len(wantBase), len(overlay))
	}
	for i, w := range wantBase {
		if cmd.Env[i] != w {
			t.Errorf("cmd.Env[%d] = %q; want inherited entry %q first", i, cmd.Env[i], w)
		}
	}

	// The appended entries sit LAST (os/exec's documented dedup-keeps-last
	// consumes them in order, so an appended binding always wins over an
	// inherited one). Map iteration order is random — assert membership of
	// the tail set, not its order.
	tail := map[string]bool{}
	for _, e := range cmd.Env[len(wantBase):] {
		tail[e] = true
	}
	for _, want := range []string{"A=override", "TMGO_PROBE=winner"} {
		if !tail[want] {
			t.Errorf("appended tail %v missing %q (overlay entries must sit last)", cmd.Env[len(wantBase):], want)
		}
	}
}

// ① env contract — real-process leg: the child sees the overlay value win
// over the inherited binding of the same key (dedup-keeps-last, end to end).
func TestStart_EnvOverlayReachesChild(t *testing.T) {
	// Not parallel: t.Setenv manipulates process env.
	t.Setenv("TMGO_T3_PROBE", "inherited")

	r := NewRunner()
	h, err := r.Start(context.Background(), tools.ProcessSpec{
		Name: helperPath,
		Args: []string{"printenv", "TMGO_T3_PROBE"},
		Env:  map[string]string{"TMGO_T3_PROBE": "winner"},
	})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	out, _ := io.ReadAll(h.Stdout())
	if err := h.Wait(); err != nil {
		t.Fatalf("Wait() returned error: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "winner" {
		t.Errorf("child saw TMGO_T3_PROBE = %q; want %q (overlay must win)", got, "winner")
	}
}

// ② fd fast path: the handed-out readers are *os.File — that concrete type
// is the os/exec fast-path trigger (direct fd inheritance, no copy goroutine).
func TestStart_HandleReadersAreOsFiles(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	h, err := r.Start(context.Background(), tools.ProcessSpec{Name: helperPath, Args: []string{"echo", "hello"}})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if _, ok := h.Stdout().(*os.File); !ok {
		t.Errorf("Stdout() = %T; want *os.File (fd fast path would not trigger)", h.Stdout())
	}
	if _, ok := h.Stderr().(*os.File); !ok {
		t.Errorf("Stderr() = %T; want *os.File (fd fast path would not trigger)", h.Stderr())
	}
	_, _ = io.Copy(io.Discard, h.Stdout())
	_, _ = io.Copy(io.Discard, h.Stderr())
	if err := h.Wait(); err != nil {
		t.Fatalf("Wait() returned error: %v", err)
	}
}

// ② wire/start interleave (ADR-074 D4 contract 1): a real two-stage chain —
// Start(spec[0]) → spec[1].Stdin = handle[0].Stdout() → Start(spec[1]) — the
// read-end is alive from Start(spec[0])'s return and start order stays 0→1.
func TestStart_TwoStagePipelineInterleave(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	ctx := context.Background()

	h1, err := r.Start(ctx, tools.ProcessSpec{Name: helperPath, Args: []string{"printf", "alpha\nbeta\ngamma\n"}})
	if err != nil {
		t.Fatalf("Start(spec[0]) returned error: %v", err)
	}

	h2, err := r.Start(ctx, tools.ProcessSpec{
		Name:  helperPath,
		Args:  []string{"grep", "alpha"},
		Stdin: h1.Stdout(), // wire from the already-started handle
	})
	if err != nil {
		t.Fatalf("Start(spec[1]) returned error: %v", err)
	}

	out, err := io.ReadAll(h2.Stdout())
	if err != nil {
		t.Fatalf("reading stage-2 stdout: %v", err)
	}
	if err := h2.Wait(); err != nil {
		t.Fatalf("stage-2 Wait() returned error: %v", err)
	}
	if err := h1.Wait(); err != nil {
		t.Fatalf("stage-1 Wait() returned error: %v", err)
	}
	if string(out) != "alpha\n" {
		t.Errorf("piped output = %q; want %q", out, "alpha\n")
	}
}

// ④ ctx-cancel-during-Wait: cancellation while Wait blocks returns an error
// promptly, bounded by the adapter's WaitDelay — not the child's runtime.
func TestStart_ContextCancelDuringWaitReturnsPromptly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := NewRunner()
	h, err := r.Start(ctx, tools.ProcessSpec{Name: helperPath, Args: []string{"sleep", "5"}})
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	time.AfterFunc(100*time.Millisecond, cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- h.Wait() }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Wait() = nil after context cancellation; want error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Wait() did not return within 3s of cancellation (WaitDelay bound violated)")
	}
}

// ⑥ Start failure: a nonexistent binary returns (nil, non-nil error) with no
// leaked readers — the close-on-Start-failure path runs cleanly.
func TestStart_NonexistentBinaryReturnsErrorWithoutHandle(t *testing.T) {
	t.Parallel()

	r := NewRunner()
	h, err := r.Start(context.Background(), tools.ProcessSpec{Name: "tmgo-t3-no-such-binary-xyz"})
	if h != nil {
		t.Errorf("Start() handle = %v; want nil", h)
	}
	if err == nil {
		t.Fatal("Start() err = nil; want non-nil for a nonexistent binary")
	}
}
