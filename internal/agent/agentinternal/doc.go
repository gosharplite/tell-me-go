// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agentinternal is a test-only delegation bridge (ADR-022).
//
// Every exported function is a one-line pass-through to
// agent.InternalAccessor. There is zero branching, zero business
// logic, and zero error handling. The bridge is exercised by tests
// in internal/agent/ (package agent_test) and tests/integration/agent/,
// which collectively cover all 18 exported symbols.
//
// This package is intentionally excluded from unit test coverage
// metrics (see the test-coverage target in the top-level Makefile).
// Writing mock-based unit tests for pure delegation methods would
// test Go's method dispatch, not project logic, and would create
// maintenance burden with zero value.
//
// See: docs/adr/2026-04-test-only-access-via-agentinternal-bridge.md
// See: https://github.com/gosharplite/tell-me-go/issues/138
package agentinternal
