# ADR-022: Test-Only Access via `agentinternal` Bridge & `*ForInternalUse` Branding

> **Update (post-merge of issue #136):** The `GetTracker` exception
> documented below was resolved. `InternalAccessor.GetTracker()` was
> renamed to `GetTrackerForInternalUse()` to bring it under the
> uniform `*ForInternalUse` brand. Production code obtains the
> tracker via `ports.SessionDependencies.GetTracker()` (a different
> interface). The "Compliance & Enforcement" section's reference to
> `scripts/check_no_test_imports.sh` is also stale: the actual CI
> guard is the `verify-internal-bridge-brand` target in the
> top-level `Makefile`. The original ADR text below is preserved
> verbatim for historical context.

**Status:** Accepted
**Date:** 2026-04
**Deciders:** Architect, Coder
**Consulted:** Reviewer
**Supersedes:** N/A
**Superseded by:** N/A
**Related:** ADR-004 (ChatterParams Elimination), ADR-007 (Agent Options Extraction), ADR-021 (Test Doubles in `*test` Sub-Packages)

## Context

For most of the project's life the `internal/agent` package shipped a
public `InternalAccessor` interface containing 11 `Get*`/`Set*` methods,
plus a `NewAgentInternal()` constructor and a `RuntimeConfigInternal`
type alias. Together they constituted a sprawling **test-only escape
hatch on the production API surface** — exactly the anti-pattern that
ADR-004 (*ChatterParams Elimination*) was written to eliminate, and
exactly the recurrence of which ADR-007 (*Agent Options Extraction*)
expected the `AgentOption` pattern to prevent.

The escape hatch existed because Go has no built-in mechanism to grant
one specific package privileged access to another package's unexported
fields without also granting it to the rest of the world. The 11
accessors served three distinct testing scenarios that the original
authors lumped together:

1. **Same-package readback** in `internal/agent/agent_lifecycle_test.go`
   (declared `package agent`). This file could already access
   unexported fields directly — it did not need any bridge at all. The
   `AsInternal(chatter).GetEvents()` pattern it used was machinery
   without purpose.
2. **Same-package external readback** in `internal/agent/agent_error_test.go`
   (declared `package agent_test`). This file constructs a deliberately
   *broken* agent — no engine, no executor — to exercise narrow error
   paths in `Chat`. It cannot use `NewAgent()` because that would
   initialize the very components it wants to remain `nil`. So it needed
   a genuine cross-package bridge.
3. **Cross-package white-box mutation** in `tests/integration/agent/`,
   which both readback typed state mid-test (e.g. asserting that a
   `Tracker` was wired correctly) *and* mutated state mid-test (e.g.
   replacing the `Tracker` and re-running `ApplyConfig` to verify
   reconfiguration). A construction-time `Builder` pattern alone cannot
   serve scenario 3 — the mutations happen long after construction is
   complete.

PR #94 (the first attempt at issue #86 / #95) tried to solve all three
scenarios with a single `agenttest.AgentBuilder` and skipped 5
integration tests with `t.Skip("TODO(#86)")` because the builder
approach could not express mid-test mutation. PR #94 also placed the
builder inside the production `agent` package, which transitively
imported `"testing"` into the production binary — a direct violation of
ADR-021. Both gaps had to be closed simultaneously to make the refactor
land.

## Decision

The codebase adopts **three complementary mechanisms** for test access
to internal `*agent` state, each scoped to one of the scenarios above.
None alone is sufficient; together they cover every legitimate test
need without re-introducing untyped `Get*`/`Set*` slop on the public
API surface.

### 1. Same-package tests — direct field access

For `_test.go` files declared `package agent`:

```go
// internal/agent/agent_lifecycle_test.go
chatter, err := NewAgent(gw, bus, reg, opts...)
a := chatter.(*agent)        // legal: same package
assert.Equal(t, bus, a.events)
```

No helper, no interface, no machinery. The previous `AsInternal +
GetEvents` round-trip is removed.

### 2. Same-package external tests — `agentinternal` bridge

For `_test.go` files declared `package agent_test` (typically because
they need to verify the public API as an external consumer would):

```go
// internal/agent/agent_error_test.go
import "github.com/gosharplite/tell-me-go/internal/agent/agentinternal"

a := agentinternal.NewBareAgent()
a.SetEventsForTest(bus)
a.SetCtxManagerForTest(&session.ContextManager{History: hm})
err := a.Chat(ctx, sess, "hello")
```

`agentinternal.NewBareAgent()` constructs an uninitialized agent
without going through `NewAgent()`. The returned `*AgentInternal`
exposes typed read-only accessors (`GetEvents`, `GetCtxManager`, …)
and a small set of clearly-suffixed `*ForTest` mutators
(`SetEventsForTest`, `SetTrackerForTest`, …). The suffix is the
brand: every call site advertises that this is non-production code.

### 3. Cross-package integration tests — same `agentinternal` bridge

`tests/integration/agent/` uses the same `agentinternal` wrapper, both
on agents constructed via `NewAgent()` (`AsAgentInternal(chatter)`)
and on bare agents (`NewBareAgent()`). The same `*ForTest` mutators
serve mid-test mutation needs.

### The `*ForInternalUse` brand on `*agent`

Because Go cannot grant cross-package access to unexported fields, the
`agentinternal` wrapper must call exported methods on `*agent`. Those
methods are kept exported but **branded** with the `*ForInternalUse`
suffix:

```go
// internal/agent/internal_bridge.go
func (a *agent) SetEventsForInternalUse(bus events.EventBus) { a.events = bus }
func (a *agent) GetCtxManagerForInternalUse() *session.ContextManager { return a.ctxManager }
// …
```

The brand makes the contract enforceable by simple grep:

```sh
# Any non-test, non-bridge file referencing ForInternalUse fails CI.
grep -rln 'ForInternalUse' --include='*.go' . \
  | grep -v _test.go \
  | grep -v 'internal/agent/internal_bridge.go' \
  | grep -v 'internal/agent/agent.go' \
  | grep -v 'internal/agent/agentinternal/'
```

This honestly admits that some plumbing must remain exported, while
making misuse trivially detectable.

### `RuntimeSnapshot` value type

`agent.RuntimeConfigInternal` (a type alias to the unexported
`runtimeConfig`) is replaced by `agentinternal.RuntimeSnapshot`, a
plain value-typed struct. The bridge converts internal config to a
snapshot on read; on write, the bridge passes the snapshot's fields to
`SetRuntimeConfigForInternalUse(...)` which constructs a fresh
internal `runtimeConfig`. Tests get a stable, copy-safe API; the
production type stays unexported.

### The `GetTracker` exception

`GetTracker` is the lone test-shaped accessor that has confirmed
**production callers**: `infrastructure/factory/chatter.go` and
`infrastructure/di/container.go`. Removing it requires a separate
refactor (tracked by issue #87). Until then, `GetTracker` stays
exported on `InternalAccessor` with a docstring that names this ADR
and the follow-up issue. The `*ForTest` wrapper adds
`SetTrackerForTest` (no production caller for the setter).

## Hard Rules

1. **No production file imports `"testing"`.**
   `eventstest/cleanup.go` and the `agentinternal` package are
   structurally exempt: they exist precisely to host helpers that
   need `"testing"`. Any other non-`_test.go` file matching
   `grep '"testing"'` fails CI.
2. **No new `With*` `AgentOption` whose docstring mentions tests.**
   Construction-time injection for tests goes through
   `agentinternal.NewBareAgent` followed by `*ForTest` mutators, not
   through `AgentOption`. (This rule prevents the `AgentOption`
   surface from becoming the next escape hatch.)
3. **No new exported `Get*`/`Set*` on `*agent` without the
   `ForInternalUse` brand.** The brand is the contract; the contract
   is enforceable by grep.
4. **No `_test.go` file under `internal/agent/` calls `*ForInternalUse`
   methods directly.** Tests go through the `agentinternal` wrapper,
   which means the production-side bridge methods have exactly one
   consumer package and can be inventoried at any time.

## Consequences

### Positive

- **`InternalAccessor` shrinks from 12 methods to 2 + bridge**
  (`ApplyConfig`, `GetTracker`). The public API surface for the
  agent's white-box state collapses to almost nothing.
- **`agent.NewAgentInternal()` and `agent.RuntimeConfigInternal`
  removed from the production package.** Both moved to the bridge as
  `agentinternal.NewBareAgent()` and `agentinternal.RuntimeSnapshot`.
- **PR #94's `t.Skip("TODO #86")` workarounds are no longer needed.**
  The 5 previously-skipped tests run by virtue of the bridge
  supporting both bare-agent construction and mid-test mutation.
- **Same-package tests become trivially readable.** Direct field
  access is the most honest possible idiom.
- **Layer rule preserved.** `agenttest/` remains a leaf with no
  upward dependency on `internal/agent`; the bridge that needs that
  dependency lives in `agentinternal/` exactly as ADR-021's "escape
  hatch" rule mandates.

### Negative

- **Method names are longer.** `SetEventsForTest` reads worse than
  `SetEvents`. This is deliberate friction: the verbosity discourages
  drive-by misuse and makes grep audits trivial.
- **One small type-conversion layer** (`RuntimeSnapshot` ↔
  `runtimeConfig`) must be maintained whenever fields are added to
  the internal config. This is the cost of not exporting the internal
  type.
- **`GetTracker` is a documented exception, not a clean removal.**
  Honest about the production leak, but not architecturally pure
  until #87 closes.

### Neutral

- **One new package file** (`internal/agent/internal_bridge.go`)
  consolidates all `*ForInternalUse` methods next to a header comment
  explaining the contract. Discoverable by file name alone.

## Alternatives Considered

1. **Single `agenttest.AgentBuilder` for all three scenarios (PR #94's
   approach).** Rejected because (a) it placed `"testing"` in the
   production package, and (b) it could not express mid-test mutation
   without re-introducing setter-style methods, defeating its own
   purpose. Lessons codified above.
2. **Move all white-box tests into `package agent` (internal).** This
   would let every test use direct field access (scenario 1 above) and
   eliminate the bridge entirely. Rejected because some tests
   legitimately verify the public API surface as an external consumer
   would, and forcing them into the production package weakens that
   verification.
3. **Use `linkname` or `unsafe` to access unexported fields from
   `agentinternal`.** Rejected as a hack that buys nothing over the
   `*ForInternalUse` brand and breaks `go vet` cleanliness.
4. **Generate the `agentinternal` wrapper from a struct tag using
   `go generate`.** Out of scope. Six wrapper methods are not enough
   surface to justify a generator; if the surface ever grows beyond
   a dozen, this is the natural next step.

## Compliance & Enforcement

- **Grep CI check** (see the `verify-internal-bridge-brand` target in
  the top-level `Makefile`):
  - No `_test.go`-excluded file may match `"testing"` outside
    `eventstest/` and `agentinternal/`.
  - No `_test.go`-excluded file outside the `agent` package's bridge
    files may match `ForInternalUse`.
- **Code review checklist** for new agent-touching PRs:
  - Does the change add a `Get*`/`Set*` to `*agent`? It must be
    `*ForInternalUse`-suffixed and consumed only by `agentinternal/`.
  - Does the change add a new `AgentOption` whose godoc mentions
    "test"? Reject; route through `agentinternal.NewBareAgent` +
    `*ForTest`.
  - Does the change add a new `_test.go` import of `"testing"` from
    a non-`_test.go` file? Reject; route through a `*test` sibling
    package or `agentinternal`.

## References

- ADR-004 (`2026-01-chatterparams-elimination.md`) — same anti-pattern,
  prior round; this ADR closes the recurrence the `Get*`/`Set*`
  accessors caused.
- ADR-007 (`2026-02-agent-options-extraction.md`) — the `AgentOption`
  pattern; rule 2 above protects it from being weaponized as the next
  escape hatch.
- ADR-021 (`2026-04-test-doubles-in-pkgtest-subpackages.md`) — the
  leaf rule and the `*internal/` escape hatch this ADR builds on.
- Issue #86 — original bug report (superseded by #95).
- Issue #95 — the v2 issue that drove this ADR.
- PR #94 — the abandoned first attempt; lessons codified above.
- Issue #87 — follow-up to refactor `chatter.go` and `container.go`
  off `GetTracker`, after which `GetTracker` itself can be moved
  behind the `*ForInternalUse` brand and removed from the public
  `InternalAccessor` interface entirely.
