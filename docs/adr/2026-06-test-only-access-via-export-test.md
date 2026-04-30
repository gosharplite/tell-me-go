# ADR-027: Test-Only Access via export_test.go and *internal Sub-Packages

**Status:** Accepted

**Date:** 2026-06

## Context

The agent package (`internal/agent`) currently exposes a `InternalAccessor` interface and
associated `Get*`/`Set*` methods on the `agent` struct for white-box testing. These methods
live directly in the production source file (`agent.go`) and are compiled into every binary.
While they are annotated with `[FOR TESTING ONLY]`, they bloat the production API surface,
increase the risk of accidental misuse in application code, and violate the principle that
test-only machinery should never leak into production builds.

Previous attempts to remove these accessors failed because:

1. The replacement pulled the `"testing"` package into production code, violating
   **ADR-021** ("Test Doubles Live in `*test` Sub-Packages").
2. Integration tests in `package agent_test` and the `agenttest` sub-package relied on
   the setters for state injection and could not be migrated atomically.
3. There was no uniform, architecture-compliant mechanism for same-package white-box
   readback versus cross-package test construction.

## Decision

We adopt a three-tier strategy that cleanly separates same-package white-box testing,
cross-package test construction, and production invariants:

### 1. Production packages MUST NOT import `"testing"`

This rule — already codified in **ADR-021** — is reaffirmed and extended. No file in a
non-`_test.go` compilation unit may import the `"testing"` package. The Go linker
cannot strip the `testing` package from binaries, so any such import permanently
contaminates the production artifact.

### 2. Same-package white-box access: `test_accessors.go`

A dedicated file `test_accessors.go` in `package agent` exports typed accessor
functions that accept `ports.Chatter` and type-assert internally to `*agent`.
These functions serve both same-package `_test.go` files and cross-package
test helpers (`agentinternal`).

**Binding rules for `test_accessors.go`:**
- Must NOT import `"testing"`.
- Must NOT contain test logic, assertions, or test helpers.
- Must only export narrow, typed accessor functions (getters and setters).
- Must use a consistent naming convention: `<Field>ForTest` for getters,
  `Set<Field>ForTest` for setters.

### 3. Cross-package white-box access: existing `*internal` sub-package

When a `*test` sub-package (e.g., `agenttest`) or an external `_test` package needs
to read or mutate internal state, it MUST route through an existing `*internal`
sub-package that wraps a minimal interface. This package is itself governed by Go's
`internal/` visibility rules and cannot be imported outside the module tree.

### 4. Forward injection: Builder in `*internal` sub-package

Test code that needs to construct an agent with specific overrides (mocks for
`EventBus`, `ContextManager`, etc.) MUST use a dedicated `AgentBuilder` in the
`agentinternal` sub-package. The builder:
- Calls the standard `NewAgent()` constructor with minimal valid options.
- Uses the exported `InternalAccessor` interface to inject test overrides.
- MUST NOT require new `AgentOption` constructors to be added to production code.

The builder MUST live in `agentinternal` rather than `agenttest` to avoid an
import cycle: `agent/*_test.go → agenttest → agent`. Since `agentinternal` is
already a leaf package that imports `agent` (see `accessor.go`), adding the
builder there is safe.

## Consequences

### Positive

- **Production binary hygiene**: Test-only accessors are stripped at compile time
  (via `test_accessors.go`) or confined to test-only sub-packages (`agenttest`).
- **Clear separation of concerns**: Same-package white-box tests use `test_accessors.go`;
  cross-package integration tests use the Builder in `agentinternal`.
- **No `"testing"` leak**: All three tiers avoid importing `"testing"` into
  non-test compilation units.
- **Incremental migration**: The existing `InternalAccessor` interface and `Get*`/`Set*`
  methods remain in `agent.go` during the transition. They will be removed in a
  follow-up phase once all call sites have migrated to the new mechanism.

### Negative

- **Two access mechanisms coexist temporarily** during migration (existing
  `InternalAccessor` + new `test_accessors.go` / `AgentBuilder`).
- **`test_accessors.go` functions are compiled into production binaries** (unlike
  true `export_test.go` which is test-only). However, they accept `ports.Chatter`
  rather than exposing `*agent`, do not import `testing`, and are clearly
  marked with `ForTest` suffixes — making accidental production misuse obvious.
- **The `AgentBuilder` must duplicate knowledge** of valid default options for
  `NewAgent()`, creating a coupling point that must be updated if the constructor
  signature changes.

## References

- **ADR-021**: Test Doubles Live in `*test` Sub-Packages, Not a Centralized `testutil`
- **ADR-017**: Black-Box Integration Test Tree at `tests/`
- **Issue #95**: Remove test-only `Get*`/`Set*` accessors from production `agent` struct
