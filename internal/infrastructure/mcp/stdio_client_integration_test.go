// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// captureHandler is a minimal slog.Handler that records mcp_stdio_stderr
// records (mcp_server + line) for assertions on child stderr output.
type captureHandler struct {
	mu      sync.Mutex
	records []stderrRecord
}

type stderrRecord struct {
	server string
	line   string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	var rec stderrRecord
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "mcp_server":
			rec.server = a.Value.String()
		case "line":
			rec.line = a.Value.String()
		}
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// hasLine reports whether a stderr record for the given server and line was
// captured.
func (h *captureHandler) hasLine(server, line string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.server == server && r.line == line {
			return true
		}
	}
	return false
}

// newCaptureLogger builds a slog.Logger backed by a captureHandler.
func newCaptureLogger() (*captureHandler, *slog.Logger) {
	h := &captureHandler{}
	return h, slog.New(h)
}

// newTestClient spawns the fixture binary with the given args and a
// per-operation timeout of timeoutSeconds (0 → the 300s default). The client
// is registered for cleanup so every test closes its child.
func newTestClient(t *testing.T, args []string, timeoutSeconds int, logger *slog.Logger) *StdioClient {
	t.Helper()
	cfg := config.MCPServerConfig{
		Command: stdioServerPath,
		Args:    args,
		Timeout: timeoutSeconds,
	}
	c, err := NewStdioClient(cfg, logger)
	if err != nil {
		t.Fatalf("NewStdioClient() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// waitForStderr polls (deadline-bounded, ADR-036 — no time.Sleep) until the
// child stderr record server/line is captured or the deadline elapses. Child
// stderr is copied asynchronously by os/exec, so assertions must wait.
func waitForStderr(t *testing.T, h *captureHandler, server, line string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if h.hasLine(server, line) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stderr record %q (server %q) not captured within %v", line, server, within)
		}
		runtime.Gosched()
	}
}

// TestStdio_RoundTrip pins the happy path over a real child process: tool
// listing, an echo round-trip, child stderr surfaced through slog (including
// the startup fixture_ready line), and stderr never parsed as JSON-RPC.
func TestStdio_RoundTrip(t *testing.T) {
	h, logger := newCaptureLogger()
	c := newTestClient(t, []string{"serve"}, 0, logger)

	defs, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(defs) < 5 {
		t.Fatalf("ListTools() returned %d tools, want >= 5", len(defs))
	}
	names := make(map[string]bool, len(defs))
	for _, d := range defs {
		names[d.Name] = true
	}
	for _, want := range []string{"echo", "slow", "die", "stderr", "block"} {
		if !names[want] {
			t.Errorf("ListTools() missing tool %q", want)
		}
	}

	res, err := c.CallTool(context.Background(), "echo", map[string]interface{}{"text": "hello"})
	if err != nil {
		t.Fatalf("CallTool(echo) error = %v", err)
	}
	if res.Text != "hello" {
		t.Errorf("echo Text = %q, want %q", res.Text, "hello")
	}
	if res.Error != nil {
		t.Errorf("echo Error = %v, want nil", res.Error)
	}

	// The fixture writes fixture_ready to stderr before serving; it must be
	// surfaced via slog with mcp_server=<stdioServerPath>.
	waitForStderr(t, h, stdioServerPath, "fixture_ready", 5*time.Second)

	// stderr output must not break the JSON-RPC protocol: a tool that writes
	// to stderr still returns its result.
	res, err = c.CallTool(context.Background(), "stderr", nil)
	if err != nil {
		t.Fatalf("CallTool(stderr) error = %v", err)
	}
	if res.Text != "stderr done" {
		t.Errorf("stderr tool Text = %q, want %q", res.Text, "stderr done")
	}
	waitForStderr(t, h, stdioServerPath, "stderr-fixture-line", 5*time.Second)
}

// TestStdio_HandshakeTimeoutKillsChild pins the constructor's bounded
// handshake: a child that never speaks on stdout fails the connect within the
// configured timeout, and the child is killed (no leak).
func TestStdio_HandshakeTimeoutKillsChild(t *testing.T) {
	c, err := NewStdioClient(config.MCPServerConfig{
		Command: stdioServerPath,
		Args:    []string{"never-init"},
		Timeout: 1,
	}, slog.Default())
	if err == nil {
		_ = c.Close() // cleanup; constructor should not have succeeded
		t.Fatal("NewStdioClient(never-init) error = nil, want connect error")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "connect")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Errorf("error = %q, want DeadlineExceeded or a deadline mention", err.Error())
	}
}

// TestStdio_GracefulEOFClose pins the graceful shutdown path: a well-behaved
// child exits on stdin EOF, Close returns nil, and a second Close is a no-op.
func TestStdio_GracefulEOFClose(t *testing.T) {
	c := newTestClient(t, []string{"serve"}, 0, slog.Default())

	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() (second) error = %v", err)
	}
}

// TestStdio_EOFIgnoringBackstop pins the kill backstop against an
// EOF-ignoring child: Close must return even though stdin EOF alone cannot
// reap the child. If this test hangs, that is a real production defect in
// StdioClient.Close (its session.Close blocking on a non-responding peer).
func TestStdio_EOFIgnoringBackstop(t *testing.T) {
	c := newTestClient(t, []string{"ignore-eof"}, 0, slog.Default())

	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	// The assertion is that this returns at all.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestStdio_LauncherTreePassThrough pins the npx/uvx pass-through shape: the
// launcher (direct child) inherits stdio to its own child, and Close reaps
// the launcher AND the grandchild via shared-pipe stdin EOF.
func TestStdio_LauncherTreePassThrough(t *testing.T) {
	h, logger := newCaptureLogger()
	c := newTestClient(t, []string{"launcher", stdioServerPath}, 0, logger)

	// The launcher prints launcher_child_pid=<pid> to stderr before waiting.
	waitForStderrLine := func() string {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, r := range h.records {
			if strings.HasPrefix(r.line, "launcher_child_pid=") {
				return r.line
			}
		}
		return ""
	}
	deadline := time.Now().Add(5 * time.Second)
	var pidLine string
	for pidLine == "" {
		pidLine = waitForStderrLine()
		if pidLine != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("launcher_child_pid never observed on stderr")
		}
		runtime.Gosched()
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(pidLine, "launcher_child_pid="))
	if err != nil {
		t.Fatalf("malformed pid line %q: %v", pidLine, err)
	}

	// The handshake and tool listing prove the stdio streams pass through the
	// launcher to the grandchild server.
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools() through launcher error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("process liveness poll is Unix-only")
	}
	// deadline-bounded liveness poll, ADR-036: the grandchild (serve) must be
	// gone within 5s, reaped via shared-pipe stdin EOF (the launcher exits on
	// EOF and takes its child with it).
	killDeadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return // ESRCH: grandchild is gone
		}
		if time.Now().After(killDeadline) {
			t.Fatalf("grandchild pid %d still alive 5s after Close", pid)
		}
		runtime.Gosched()
	}
}

// TestStdio_ChildDeathMidSession pins the fast-death pre-check stickiness: a
// child that dies mid-session surfaces as a terminal error on the first call
// and as the identical sticky exit error on every subsequent call.
func TestStdio_ChildDeathMidSession(t *testing.T) {
	c := newTestClient(t, []string{"serve"}, 0, slog.Default())

	// The die tool exits the child process; the first call errors with
	// EOF/connection-closed, possibly annotated with the child-exit wrap
	// (accept either — the annotation races the reaper).
	_, err1 := c.CallTool(context.Background(), "die", nil)
	if err1 == nil {
		t.Fatal("CallTool(die) error = nil, want error")
	}
	lower := strings.ToLower(err1.Error())
	if !strings.Contains(err1.Error(), "exited") && !strings.Contains(lower, "eof") && !strings.Contains(lower, "closed") {
		t.Errorf("first call error = %q, want a child-death indication (exited/EOF/closed)", err1.Error())
	}

	// The second call fails fast via the sticky pre-check: its error must
	// carry the child-exit wrap.
	_, err2 := c.CallTool(context.Background(), "echo", map[string]interface{}{"text": "x"})
	if err2 == nil {
		t.Fatal("CallTool after child death error = nil, want error")
	}
	if !strings.Contains(err2.Error(), "exited") {
		t.Errorf("second call error = %q, want child-exit wrap", err2.Error())
	}

	// The third call returns the identical sticky error value (pointer
	// equality — the pre-check short-circuits on c.exitErr).
	_, err3 := c.CallTool(context.Background(), "echo", map[string]interface{}{"text": "x"})
	if err3 != err2 {
		t.Error("third call error != second call error; expected the identical sticky error value")
	}
}

// TestStdio_WedgeTimesOut pins the wedge failure mode: a server that never
// responds surfaces as context.DeadlineExceeded (distinguishable from a child
// death by error text).
func TestStdio_WedgeTimesOut(t *testing.T) {
	c := newTestClient(t, []string{"serve"}, 1, slog.Default())

	_, err := c.CallTool(context.Background(), "block", nil)
	if err == nil {
		t.Fatal("CallTool(block) error = nil, want DeadlineExceeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("CallTool(block) error = %v, want context.DeadlineExceeded", err)
	}
}

// TestStdio_SlowButAlive_NoKill pins the no-kill-on-operation-timeout policy:
// an operation that exceeds the per-call timeout must NOT kill the child — a
// subsequent call on the same client succeeds.
func TestStdio_SlowButAlive_NoKill(t *testing.T) {
	c := newTestClient(t, []string{"serve"}, 1, slog.Default())

	_, err := c.CallTool(context.Background(), "slow", map[string]interface{}{"ms": 1500})
	if err == nil {
		t.Fatal("CallTool(slow 1500ms) with 1s timeout error = nil, want DeadlineExceeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("CallTool(slow) error = %v, want context.DeadlineExceeded", err)
	}

	// The child survived the timeout: a fresh call succeeds.
	res, err := c.CallTool(context.Background(), "echo", map[string]interface{}{"text": "ok"})
	if err != nil {
		t.Fatalf("CallTool(echo) after timeout error = %v (child was killed?)", err)
	}
	if res.Text != "ok" {
		t.Errorf("echo Text = %q, want %q", res.Text, "ok")
	}
}

// TestStdio_TwoClientsIndependent pins that two StdioClients on separate
// child processes do not share stdio state: both list tools, both serve
// calls, both close cleanly.
func TestStdio_TwoClientsIndependent(t *testing.T) {
	c1 := newTestClient(t, []string{"serve"}, 0, slog.Default())
	c2 := newTestClient(t, []string{"serve"}, 0, slog.Default())

	if _, err := c1.ListTools(context.Background()); err != nil {
		t.Fatalf("client1 ListTools() error = %v", err)
	}
	if _, err := c2.ListTools(context.Background()); err != nil {
		t.Fatalf("client2 ListTools() error = %v", err)
	}

	res1, err := c1.CallTool(context.Background(), "echo", map[string]interface{}{"text": "one"})
	if err != nil {
		t.Fatalf("client1 CallTool(echo) error = %v", err)
	}
	res2, err := c2.CallTool(context.Background(), "echo", map[string]interface{}{"text": "two"})
	if err != nil {
		t.Fatalf("client2 CallTool(echo) error = %v", err)
	}
	if res1.Text != "one" || res2.Text != "two" {
		t.Errorf("echo results = %q / %q, want one / two", res1.Text, res2.Text)
	}

	if err := c1.Close(); err != nil {
		t.Fatalf("client1 Close() error = %v", err)
	}
	if err := c2.Close(); err != nil {
		t.Fatalf("client2 Close() error = %v", err)
	}
}
