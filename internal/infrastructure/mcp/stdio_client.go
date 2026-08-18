// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// StdioClient implements tools.MCPClient for a local MCP server spawned as a
// stdio child process (COMMAND + ARGS). The child is spawned eagerly in
// NewStdioClient; the MCP handshake runs inside the constructor bounded by the
// server's EffectiveTimeout. Lifecycle: a reaper goroutine reaps the direct
// child the moment it dies (no zombies); Close sends stdin EOF first, then
// cancels as the kill backstop, then joins the reaper.
type StdioClient struct {
	name    string // cfg.Command (adapter error text carries this at most — never ARGS/ENV values)
	command string // resolved executable path
	args    []string
	dir     string
	env     []string // os.Environ() + cfg.Env as K=V, sorted keys appended last (last-wins)
	timeout time.Duration
	logger  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	sdk      *sdkmcp.Client
	session  *sdkmcp.ClientSession
	waitDone chan error // buffered 1; reaper sends cmd.Wait()'s result exactly once
	exitErr  error      // sticky wrapped child-exit error set by the fast-death pre-check
	closed   bool
}

// Compile-time assertion that StdioClient satisfies the domain port.
var _ tools.MCPClient = (*StdioClient)(nil)

// NewStdioClient spawns COMMAND as a stdio child process and completes the MCP
// handshake against it, bounded by the server's EffectiveTimeout. On any
// failure after Start, the child is cancelled and reaped before returning —
// a failed handshake never leaks a child process.
func NewStdioClient(cfg config.MCPServerConfig, logger *slog.Logger) (*StdioClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.EffectiveTimeout()
	ctx, cancel := context.WithCancel(context.Background())

	resolved, err := resolveCommand(cfg.Command)
	if err != nil {
		cancel()
		return nil, err
	}

	cmd := exec.CommandContext(ctx, resolved, cfg.Args...)
	cmd.Dir = cfg.Dir
	// Appended pairs override os.Environ() duplicates for the child (last-wins
	// in the exec environment); keys are sorted so the child env is
	// deterministic regardless of map iteration order.
	cmd.Env = append(os.Environ(), sortedEnvPairs(cfg.Env)...)

	// Both pipes must be obtained before Start; any error here leaves nothing
	// to clean up (no child exists yet).
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	cmd.Stderr = newSlogWriter(logger, cfg.Command)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	// Reaper started immediately after Start: zombie prevention is not
	// contingent on Close ever being called.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	sdk := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "tell-me-go",
		Version: "dev",
	}, nil)

	connectCtx, cancelConnect := context.WithTimeout(ctx, timeout)
	session, err := sdk.Connect(connectCtx, &sdkmcp.IOTransport{Reader: stdout, Writer: stdin}, nil)
	cancelConnect()
	if err != nil {
		// Self-clean, never leak the child: cancel kills the direct child via
		// CommandContext, then join the reaper before returning.
		cancel()
		<-waitDone
		return nil, fmt.Errorf("mcp stdio connect: %w", err)
	}

	c := &StdioClient{
		name:     cfg.Command,
		command:  resolved,
		args:     cfg.Args,
		dir:      cfg.Dir,
		env:      append(os.Environ(), sortedEnvPairs(cfg.Env)...),
		timeout:  timeout,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
		sdk:      sdk,
		session:  session,
		waitDone: waitDone,
	}
	return c, nil
}

// ListTools queries the MCP server for available tools.
func (c *StdioClient) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("mcp: client is closed")
	}
	if err := c.childExitErrLocked(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	ctx, cancel := c.operationContext(ctx)
	defer cancel()

	res, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		return nil, c.annotateIfDead(fmt.Errorf("mcp list tools: %w", err))
	}

	defs := make([]tools.MCPToolDefinition, 0, len(res.Tools))
	for _, t := range res.Tools {
		raw, err := marshalInputSchema(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools %q: %w", t.Name, err)
		}
		defs = append(defs, tools.MCPToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: raw,
		})
	}
	return defs, nil
}

// CallTool executes a tool on the MCP server with the provided arguments.
func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return tools.ToolResult{}, errors.New("mcp: client is closed")
	}
	if err := c.childExitErrLocked(); err != nil {
		c.mu.Unlock()
		return tools.ToolResult{}, err
	}
	c.mu.Unlock()

	ctx, cancel := c.operationContext(ctx)
	defer cancel()

	res, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return tools.ToolResult{}, c.annotateIfDead(fmt.Errorf("mcp call %s: %w", name, err))
	}

	text, binaries, metadata := convertCallToolResult(res)

	// Three-way split (ADR-022 / issue #1373): an MCP-level tool error is a
	// non-terminal domain outcome surfaced through ToolResult.Error with a nil
	// Go error, so the LLM can recover in-turn. Transport/JSON-RPC errors are
	// terminal Go errors (returned above).
	if res.IsError {
		return tools.ToolResult{
			Text:     text,
			Error:    fmt.Errorf("%s", toolErrorSummary(name, text)),
			Metadata: metadata,
		}, nil
	}

	return tools.ToolResult{
		Text:       text,
		BinaryData: binaries,
		Metadata:   metadata,
	}, nil
}

// Close terminates the child process and releases all resources. It is
// idempotent and concurrency safe: stdin EOF first (a well-behaved child
// exits gracefully), then cancel as the kill backstop, then join the reaper.
func (c *StdioClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	// (1) stdin EOF first: closing the transport closes both pipes, so a
	// well-behaved child (and its tree, via the shared pipe) exits
	// gracefully. (2) cancel() is the kill backstop for children that ignore
	// EOF — called explicitly before the join AND deferred so it fires even
	// if session.Close unwinds with an error (cancel is idempotent).
	defer c.cancel()

	var closeErr error
	if c.session != nil {
		if err := c.session.Close(); err != nil {
			closeErr = err
		}
	}
	c.cancel()

	// (3) join the reaper — post-cancel, cmd.Wait returns promptly (SIGKILL
	// delivered by CommandContext). If the fast-death pre-check already
	// reaped the child, exitErr holds the result.
	waitErr := c.exitErr
	if waitErr == nil {
		waitErr = <-c.waitDone
	}
	if waitErr != nil && !isExpectedKill(waitErr) {
		c.logger.Debug("mcp_stdio_child_wait", "server", c.name, "error", waitErr)
	}
	return closeErr
}

// operationContext applies the configured per-operation timeout to ctx.
// Non-positive timeout passes the original context through unchanged.
func (c *StdioClient) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}

// childExitErrLocked returns the sticky child-exit error, or nil while the
// child is alive. Non-blocking select on waitDone; on the first detection the
// wrapped error is stashed in c.exitErr so every subsequent call fails fast
// with the same error (the SDK alone does not guarantee this — its read loop
// terminates on EOF and teardown can race). Caller must hold c.mu.
func (c *StdioClient) childExitErrLocked() error {
	if c.exitErr != nil {
		return c.exitErr
	}
	select {
	case err := <-c.waitDone:
		c.exitErr = fmt.Errorf("mcp: stdio child %q exited: %w", c.command, err)
		return c.exitErr
	default:
		return nil
	}
}

// annotateIfDead annotates an in-flight operation error when it coincides with
// the child having exited (io.EOF / sdkmcp.ErrConnectionClosed + reaper
// confirms exit). A wedge surfaces as the plain context.DeadlineExceeded wrap
// — the two failure modes stay distinguishable by error text and latency.
// Takes c.mu internally; do NOT call while holding c.mu.
func (c *StdioClient) annotateIfDead(err error) error {
	if err == nil {
		return nil
	}
	c.mu.Lock()
	childErr := c.childExitErrLocked()
	c.mu.Unlock()
	if childErr == nil {
		return err
	}
	if errors.Is(err, io.EOF) || errors.Is(err, sdkmcp.ErrConnectionClosed) {
		return fmt.Errorf("%w: %v", childErr, err)
	}
	return err
}

// isExpectedKill reports whether a cmd.Wait error is the expected outcome of
// Close's cancel backstop: nil, context.Canceled, or an exec.ExitError whose
// message contains "signal: killed" (Unix SIGKILL). Other errors are logged
// at debug, never surfaced from Close.
func isExpectedKill(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
		return strings.Contains(exitErr.ProcessState.String(), "signal: killed")
	}
	return strings.Contains(err.Error(), "signal: killed")
}

// resolveCommand applies the documented resolution contract: a COMMAND
// containing a path separator is used as-is (relative paths resolve against
// DIR, per exec); a bare COMMAND is resolved via exec.LookPath in tell-me-go's
// own process PATH — not ENV.PATH, not DIR. ENV.PATH governs the child only
// post-exec. Windows bare-name lookup honors PATHEXT via the same stdlib path.
func resolveCommand(command string) (string, error) {
	if strings.ContainsAny(command, `/\`) {
		return command, nil
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("mcp stdio: command %q not found in tell-me-go's PATH (COMMAND resolves before ENV is applied; use an absolute path, a DIR-relative path, or ${VAR}-expanded COMMAND): %w", command, err)
		}
		return "", fmt.Errorf("mcp stdio: resolve command %q: %w", command, err)
	}
	return resolved, nil
}

// sortedEnvPairs converts cfg.Env into deterministic "K=V" pairs with keys
// sorted. An empty map yields nil.
func sortedEnvPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+env[k])
	}
	return pairs
}

// slogWriter routes child stderr to slog at Info with mcp_server=<name>,
// line-buffered. It is the operator's visibility window for wedged/dead
// children and is never parsed as JSON-RPC. Documented in the ADR as outside
// mcp-token-not-logged: the invariant governs tell-me-go's own credential
// plumbing, not arbitrary child output.
type slogWriter struct {
	logger *slog.Logger
	name   string
	buf    []byte
}

// newSlogWriter constructs the stderr writer for a child whose COMMAND is
// name (used in the mcp_server log attribute).
func newSlogWriter(logger *slog.Logger, name string) *slogWriter {
	return &slogWriter{logger: logger, name: name}
}

func (w *slogWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.logger.Info("mcp_stdio_stderr", "mcp_server", w.name, "line", line)
	}
	// A trailing partial line at process exit is not flushed: cmd.Stderr
	// writers receive no Close, so there is no hook to flush on.
	return len(p), nil
}
