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
// This package includes mechanical delegation tests in accessor_test.go
// that verify interface conformance between AgentInternal wrappers and
// the underlying InternalAccessor bridge. The tests use testify mocks
// (mockInternalAccessor, mockChatter, mockCostTracker) and serve as a
// regression safety net: a renamed field, a removed interface method,
// or a signature mismatch in the delegation chain is caught immediately
// rather than surfacing as a confusing nil-pointer panic in downstream
// integration tests.
//
// See: docs/adr/2026-04-test-only-access-via-agentinternal-bridge.md
// See: https://github.com/gosharplite/tell-me-go/issues/138
package agentinternal
