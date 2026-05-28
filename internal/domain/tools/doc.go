// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools defines the domain model for the tool execution system.
//
// # Core Concepts
//
// Tools are functions callable by the LLM during a conversation turn. Each
// tool has a ToolDeclaration (name, description, JSON Schema parameters,
// consent requirements) and a handler function (ToolFunc).
//
// # Tool Execution Lifecycle
//
//  1. The LLM requests a tool call via a FunctionCall in its response.
//  2. The orchestrator looks up the tool in the Registry and invokes it
//     via ToolExecutor.Execute.
//  3. The tool handler (ToolFunc) runs, optionally sending heartbeat
//     signals on the provided channel to indicate liveness.
//  4. ZombieTool monitors long-running tools and reports stalled
//     executions to the ExecutionObserver.
//  5. The tool's output (ToolResult) is formatted and sent back to the LLM.
//
// # Concurrency Model
//
// Tools may be executed concurrently unless ToolOptions.Serial is set.
// Long-running tools (ToolOptions.LongRunning) are exempt from default
// timeouts. Heartbeat monitoring (ToolOptions.LivenessThreshold) detects
// hung tools — a ZombieTool declares the execution timed out if no
// heartbeat is received within the threshold.
//
// # Registry Architecture
//
// The Registry is the central tool catalog, composed of three role
// interfaces:
//   - ToolRegistrar: adds tools to the registry (startup/wiring phase)
//   - ToolExecutor: invokes tools at runtime
//   - ToolMetadataProvider: lists available tools for LLM function calling
//
// Toolkits allow grouping related tools (e.g., "git", "file", "network")
// for lazy-loading and scoped declaration requests.
//
// # Error Conventions
//
// Sentinel errors (ErrNotImplemented, ErrUserDeclined, ErrSecurityPolicy)
// signal terminal failures that the orchestrator must not retry.
// ToolFunc implementations return errors in ToolResult.Error for
// tool-specific failures that may be reported to the LLM.
package tools
