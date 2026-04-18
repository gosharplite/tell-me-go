<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# tell-me-go — End-to-End and Integration Tests

## Overview

This directory holds the project's **black-box** test tree. Every file here imports
`github.com/gosharplite/tell-me-go/internal/...` packages from the *outside* — the same
way `cmd/tell-me-go` and any future external consumer would. As a consequence, every
exported symbol referenced from this tree is part of the **de-facto public surface** of
its package and must not be unexported, renamed, or removed without first refactoring the
callers in `tests/`.

This tree is *not* a place for unit tests; in-package `_test.go` files (white-box) remain
the default for those. See [ADR-017](../docs/adr/2026-04-blackbox-integration-test-tree.md)
for the full rationale.

## Directory layout

```text
tests/
├── e2e/                              # Full-binary end-to-end scenarios
└── integration/
    └── agent/
        ├── executor/                 # Tool-execution collaboration
        ├── orchestrator/             # Turn-engine collaboration
        └── session/                  # Session/context-manager collaboration
```

## Test categories

| Category | Path | Package declaration | Scope | When to add a test here |
| --- | --- | --- | --- | --- |
| E2E | `tests/e2e/` | `package e2e` | Full binary, real subprocess, real I/O, CLI flags | Cross-cutting CLI behavior, signal handling, deadlocks, pipe redirection |
| Integration | `tests/integration/agent/` | `package agent_test` (black-box) | Cross-package collaboration of agent components | Multi-component behavior that no single unit test can reproduce |
| Integration | `tests/integration/agent/executor/` | `package executor_test` (black-box) | Tool executor + decorators + concurrency | Stress, robustness, and table-driven executor scenarios |
| Integration | `tests/integration/agent/orchestrator/` | `package orchestrator_test` (black-box) | Turn engine + event bus + circuit breaker | Loop bounds, error propagation, limit enforcement |
| Integration | `tests/integration/agent/session/` | `package session_test` (black-box) | Session, context manager, summarization, archiving | Real concurrent state transitions across session boundaries |

## The black-box convention

- Tests in this tree **import `internal/...` packages from the outside**; they do not
  share package scope with the code under test.
- Any symbol referenced here is part of the **public contract** of its package, on equal
  footing with symbols referenced from `cmd/` or another `internal/` package.
- Before unexporting, deleting, or renaming an exported symbol, run:

  ```bash
  grep -rn "<SymbolName>" tests/
  ```

  If it appears, the change is a **breaking change** and must be planned accordingly.
- Static-analysis tools (dead-code analyzers, dependency graphs) used to justify a
  refactor MUST be scoped to the entire module (`./...`), never to a subset like
  `./internal`. A subset scope will produce false-positive "unused export" findings for
  every symbol consumed from this tree.

## Running the tests

```bash
# Integration tests with the race detector
go test -race ./tests/integration/...

# End-to-end tests with the race detector
go test -race ./tests/e2e/...

# Both, together with every other test in the module
make test          # standard
make test-race     # race-enabled, package-by-package
```

See the [Makefile](../Makefile) for the full set of targets.

## Related documents

- [ADR-017: Black-Box Integration Test Tree at `tests/`](../docs/adr/2026-04-blackbox-integration-test-tree.md)
- [Testing SOP](../docs/sop/standards/testing_standards.md)
- [ADR Index](../docs/adr/README.md)
