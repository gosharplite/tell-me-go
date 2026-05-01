// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

/*
Package context implements the context preparation pipeline for the chat session.

It encapsulates the construction, validation, and pruning of LLM input context
through a chain of ports.ContextTransformer instances. The package is consumed
by the parent session package and by internal/agent.

Entry points:

  - Manager       — orchestrates the pipeline; primary façade.
  - Strategy      — token-budget and limits accounting.
  - Factory       — builds a standard transformer pipeline for a given limits set.

This package does not depend on any sibling under internal/agent/session.
The reverse direction (session → session/context) is the only allowed coupling,
maintained explicitly by session/internal_tools.go and session/skill_injector.go.

See ADR-026 for the decomposition rationale.
*/
package context
