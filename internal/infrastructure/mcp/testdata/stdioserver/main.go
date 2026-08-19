// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Command stdioserver is the SDK fixture server for the StdioClient
// integration suite. It is built by TestMain and spawned as a real child
// process; subcommands are selected via os.Args[1]:
//
//	serve        canonical fixture: MCP server over stdin/stdout
//	launcher     execs <path> serve with inherited stdio (pass-through pin)
//	never-init   writes nothing to stdout, blocks forever (handshake timeout)
//	ignore-eof   serves normally but ignores stdin EOF (kill-backstop pin)
//	list-error   serves normally but fails every tools/list request (session
//	             error while the child stays alive)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: stdioserver <serve|launcher|never-init|ignore-eof|list-error>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve()
	case "launcher":
		launcher()
	case "never-init":
		neverInit()
	case "ignore-eof":
		ignoreEOF()
	case "list-error":
		listError()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}

// newFixtureServer builds the canonical SDK MCP server with the seven fixture
// tools: echo, slow, die, stderr, block, args-capture, fail.
func newFixtureServer() *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "stdioserver-fixture", Version: "1.0.0"}, nil)

	textOnly := map[string]any{
		"type":       "object",
		"properties": map[string]any{"text": map[string]any{"type": "string"}},
	}

	server.AddTool(&sdkmcp.Tool{Name: "echo", Description: "echoes the text argument back", InputSchema: textOnly},
		func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return toolError("echo: bad arguments"), nil
			}
			text, ok := args["text"].(string)
			if !ok {
				return toolError("echo: missing text"), nil
			}
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}}, nil
		})

	server.AddTool(&sdkmcp.Tool{Name: "slow", Description: "sleeps ms milliseconds then returns", InputSchema: map[string]any{
		"type":       "object",
		"properties": map[string]any{"ms": map[string]any{"type": "number"}},
	}}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Ms float64 `json:"ms"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return toolError("slow: bad arguments"), nil
		}
		time.Sleep(time.Duration(args.Ms) * time.Millisecond)
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "slow done"}}}, nil
	})

	server.AddTool(&sdkmcp.Tool{Name: "die", Description: "exits the process immediately", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			os.Exit(0)
			return nil, nil // unreachable
		})

	server.AddTool(&sdkmcp.Tool{Name: "stderr", Description: "writes a fixed line to stderr and returns", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			fmt.Fprintln(os.Stderr, "stderr-fixture-line")
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "stderr done"}}}, nil
		})

	server.AddTool(&sdkmcp.Tool{Name: "block", Description: "blocks forever (wedge test)", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			select {}
			return nil, nil // unreachable
		})

	server.AddTool(&sdkmcp.Tool{Name: "args-capture", Description: "returns the raw arguments bytes as text", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			text := string(req.Params.Arguments) // json.RawMessage; pre-fix this is "null"
			if text == "" {
				text = "{}" // defensive default for a genuinely-absent field
			}
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}}, nil
		})

	server.AddTool(&sdkmcp.Tool{Name: "fail", Description: "returns an MCP-level tool error (isError)", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return toolError("fail: deliberate tool error"), nil
		})

	return server
}

func toolError(text string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}}
}

// serve runs the canonical fixture server over stdin/stdout until the client
// closes the connection (stdin EOF), at which point server.Run returns and the
// process exits cleanly. A SIGTERM/SIGINT also cancels the run.
func serve() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := newFixtureServer()
	fmt.Fprintln(os.Stderr, "fixture_ready")
	if err := server.Run(ctx, &sdkmcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}

// launcher execs <servePath> serve with all three stdio streams inherited —
// no new pipes — pinning the npx/uvx pass-through shape. The child pid is
// printed to stderr so the integration test can poll its liveness.
func launcher() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: stdioserver launcher <servePath>")
		os.Exit(2)
	}
	servePath := os.Args[2]

	cmd := exec.Command(servePath, "serve")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "launcher start: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "launcher_child_pid=%d\n", cmd.Process.Pid)

	err := cmd.Wait()
	if err == nil {
		return
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		os.Exit(exitErr.ExitCode())
	}
	fmt.Fprintf(os.Stderr, "launcher wait: %v\n", err)
	os.Exit(1)
}

// neverInit writes nothing to stdout and blocks forever: the MCP handshake
// never completes, pinning the constructor's handshake-timeout kill path.
func neverInit() {
	select {}
}

// ignoreEOF is a functioning MCP server that completes the handshake and
// serves tools but, unlike serve, does NOT exit when stdin reaches EOF: on
// session close it blocks forever instead of returning. This pins the
// client's Close kill backstop (stdin EOF alone cannot reap this child).
func ignoreEOF() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := newFixtureServer()
	ss, err := server.Connect(ctx, &sdkmcp.StdioTransport{}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ignore-eof connect: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "fixture_ready")

	ssClosed := make(chan error, 1)
	go func() {
		ssClosed <- ss.Wait()
	}()

	select {
	case <-ctx.Done():
		ss.Close()
		<-ssClosed
	case <-ssClosed:
		// stdin EOF — a normal server returns here; this fixture ignores EOF
		// and blocks forever.
		select {}
	}
}

// listError is a functioning MCP server that completes the handshake and
// serves tools but returns a JSON-RPC error for every tools/list request,
// pinning the client's ListTools session-error branch: the child stays alive
// (a session error, not a death), so tools/call keeps working afterwards.
func listError() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := newFixtureServer()
	// Intercept only tools/list; the initialize handshake, notifications, and
	// tools/call all pass through to the SDK's normal handlers. The returned
	// error becomes a JSON-RPC error response on the wire.
	server.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method == "tools/list" {
				return nil, fmt.Errorf("tools/list denied by list-error fixture")
			}
			return next(ctx, method, req)
		}
	})
	fmt.Fprintln(os.Stderr, "fixture_ready")
	if err := server.Run(ctx, &sdkmcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "list-error: %v\n", err)
		os.Exit(1)
	}
}
