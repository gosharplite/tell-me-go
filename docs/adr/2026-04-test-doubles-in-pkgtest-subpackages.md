# ADR-021: Test Doubles Live in `*test` Sub-Packages, Not a Centralized `testutil`

**Status:** Accepted
**Date:** 2026-04
**Deciders:** Architect, Coder
**Consulted:** Reviewer, Tester
**Supersedes:** N/A
**Superseded by:** N/A
**Related:** ADR-017 (Black-Box Integration Test Tree)

## Context

For roughly the first year of this project, all test doubles (mocks, fakes,
stubs, helpers) were dumped into a single package: `internal/domain/testutil`.
This package grew unchecked to **1,664 LOC across 12 files** and contained
**39 exported symbols** spanning every architectural ring of the system —
agent mocks, event bus mocks, UI renderer mocks, security mocks, tool
mocks, persistence factories, and generic primitives like a thread-safe
buffer.

Three concrete pathologies emerged:

1. **Domain-layer pollution.** A package that lived under `internal/domain/`
   imported `bytes`, `sync`, mock frameworks, and the testify library, and
   was depended on by ~60 test files spanning every other layer. The domain
   layer is supposed to be the architectural floor — pure business types
   with minimal dependencies. `testutil` violated that contract by sitting
   inside it.
2. **Layer violations were invisible.** Because `testutil` was in `domain/`,
   any layer above it (agent, infrastructure, tools, ui) could legally
   import it. This let helpers carry hidden upward-dependency smells. For
   example, `MockSecurityManager` was consumed almost exclusively from
   `internal/tools/...`, but flowed through the domain layer to get there
   — masking what should have been a `tools → tools` dependency. When the
   `testutil` qualifier was rewritten to its true destination during
   Session 7, `verify_architecture` immediately surfaced a layer violation
   that had been silently latent for months.
3. **Misleading import aliases bred.** Three files aliased
   `internal/domain/testutil` as `inframock`, actively *advertising* the
   smell that a test was importing the production persistence package
   when in fact it was importing the testutil dump. Two more files used
   `infrapersistence` as the alias for the same reason. These aliases
   masked refactor opportunities and complicated bulk renames.

A 7-session refactor (Sessions 1–8, January–April 2026) dissolved the dump
entirely. The destination shape needed to be standardized as the convention
to prevent re-aggregation by well-meaning future contributors.

## Decision

**Test doubles live in `*test` sub-packages, sibling to the production
package they fake. No centralized `testutil` package may be reintroduced.**

Concretely:

| Production package | Test-double sub-package |
|---|---|
| `internal/infrastructure/persistence` | `internal/infrastructure/persistence/persistencetest` |
| `internal/agent` | `internal/agent/agenttest` |
| `internal/domain/events` | `internal/domain/events/eventstest` |
| `internal/tools` | `internal/tools/toolstest` |

For test primitives that are genuinely cross-cutting (used by multiple
unrelated packages and not specific to any one production package),
the destination is a single shared package:

| Use case | Package |
|---|---|
| Generic primitives (thread-safe buffer, sync writer, etc.) | `internal/pkg/testfixtures` |

This naming convention follows the Go standard library precedent:
`net/http/httptest`, `testing/iotest`, `testing/quick`. The rule is:
**the package being faked is named in the path; the suffix `test`
identifies the package as a test-helper sibling.**

### Architectural rules

1. **The leaf rule.** A `*test` sub-package may not import its parent
   production package (e.g. `agenttest` may not import `internal/agent`).
   This prevents import cycles when the production package's own tests
   want to use the helpers, and it prevents the `*test` package from
   accidentally pulling production code into the dependency closure of
   tests that don't need it.
2. **The escape hatch.** When a test helper *legitimately* needs to
   import the parent production package (e.g. it wraps an unexported
   accessor only available to that package), it lives in a regular
   sibling package named `<pkg>internal/` — not `<pkg>test/`. Example:
   `internal/agent/agentinternal/`. This keeps the leaf rule absolute
   for `*test` packages while still permitting privileged helpers.
3. **Production code never imports a `*test` package or `testfixtures`.**
   Enforcement is by convention plus a CI grep check; violation breaks
   the build.
4. **Self-tests use the external test package.** When a `*test`
   sub-package ships its own test suite, those tests are written as
   `package <name>_test` (the standard Go idiom for testing a public
   API surface). Internal types may use `package <name>` if the test
   needs access to unexported helpers.

### Naming convention for test doubles

- **Mocks** named after the interface they implement: `MockGateway`
  fakes `Gateway`, `MockEventBus` fakes `EventBus`.
- **Fakes** that are real, simplified production-quality implementations
  use a clarifying prefix: `NewPlainOSFileSystem` (vs the production
  `NewOSFileSystem`) — the prefix advertises that the fake omits
  features (retries, atomic writes) that production has.
- **Generic primitives** in `testfixtures/` keep the API surface area
  minimal. Interface contracts and concrete types may be unexported
  when consumers only ever call constructors (`NewSafeBuffer` returns
  an unexported `buffer` interface backed by an unexported `safeBuffer`
  struct).

### Mock construction pattern

Test doubles in `*test/` packages **must** use the hand-rolled function-field
pattern. Embedding `github.com/stretchr/testify/mock.Mock` is **prohibited**.

#### Standard pattern

A mock is a struct where each field controls the behaviour of a method, and
the zero value is immediately usable:

```go
// MockClock is a test double for clock.Clock.
// The zero value is usable — Now() falls through to real wall-clock time.
type MockClock struct {
    CurrentTime time.Time  // non-zero fixes the value returned by Now()
}
func (m *MockClock) Now() time.Time {
    if m.CurrentTime.IsZero() {
        return time.Now()
    }
    return m.CurrentTime
}
```

#### Spy variant for call-count/call-order assertion

When a test needs to assert that a method was called (or called N times, or
called in a specific order), add lightweight spy fields to the struct:

```go
type MockClock struct {
    CurrentTime   time.Time
    CalledNow     int       // call-count field
    CalledMethods []string  // call-order field (append method name on each call)
}

func (m *MockClock) Now() time.Time {
    m.CalledNow++
    m.CalledMethods = append(m.CalledMethods, "Now")
    // ... normal behaviour ...
}
```

Tests assert directly on the spy fields:

```go
mClock := &agenttest.MockClock{CurrentTime: fixedTime}
// ... run system under test ...
if mClock.CalledNow < 1 {
    t.Errorf("expected Now() to be called at least once, got %d", mClock.CalledNow)
}
```

#### Rationale

1. **Make the zero value useful** (Go proverb). Hand-rolled mocks work without
   `.On()` boilerplate.
2. **Consistent with `toolstest/`** which already uses this pattern exclusively.
3. **Simpler to read and maintain.** A `MockClock` with a `CurrentTime` field
   is self-documenting. A testify mock requires reading `.On()` calls in each
   test to understand behaviour.
4. **For call-count assertions, use spy fields** (e.g. `CalledNow int`,
   `CalledMethods []string`) rather than pulling in `testify/mock`.

#### Exception: `agentinternal/`

The `internal/agent/agentinternal/` package (ADR-022 escape hatch) may use
`testify/mock` for mocks that reference `internal/agent` package types, since
those mocks cannot live in `agenttest/` without creating import cycles.

## Consequences

### Positive

- **Domain layer is pure.** No test scaffolding sits beneath
  `internal/domain/`. `verify_architecture` enforces this structurally.
- **Layer violations surface immediately.** When a relocation moves a
  helper through a layer it shouldn't be in, `verify_architecture`
  fails at the first commit, not after months of latent debt.
- **Misleading aliases cannot survive.** A test that imports
  `agenttest.MockGateway` advertises clearly what it is importing;
  there is no plausible alias that improves on the explicit name.
- **`go doc` works.** Each helper is in a small, focused package with
  a clear GoDoc. Compare to a 1,664-line dump where finding a single
  helper required `grep`.
- **Future contributors have a template.** When a new production
  package needs test doubles, the destination is mechanical:
  `<path>/<pkg>/<pkg>test/`.

### Negative

- **More packages to navigate.** The codebase now has 5 test-helper
  packages (`persistencetest`, `agenttest`, `eventstest`, `toolstest`,
  `testfixtures`) plus `agentinternal`, instead of one. Mitigation: the
  package name itself describes its scope; `go doc <pkg>test` is the
  discovery path.
- **Bulk relocations are noisy.** Splitting a single file into per-symbol
  files inflates file count. Mitigation: one type per file is only
  enforced for non-trivial mocks; small related primitives may share a
  file (`safebuffer.go` holds the interface + struct + constructor).
- **A `MockX` type may legitimately exist in two `*test` packages**
  (e.g. `securitytest.MockInteractor` and `toolstest.MockInteractor`),
  if the same interface is mocked at two different layers with different
  semantics. This is acceptable and even desirable — the package
  qualifier disambiguates. GoDoc must explain why both exist.

### Neutral

- The refactor reduced `testutil` from 1,664 → 0 LOC across 8 sessions,
  rewriting ~700 references in ~85 files. No production code was
  changed; this was purely a relocation of test scaffolding.

## Alternatives Considered

1. **Keep a centralized `testutil` but move it to `internal/testutil/`
   (out of the domain layer).** Rejected because it preserves the
   underlying problem: a single dump that any layer can import,
   forever. Layer violations remain invisible. Future contributors
   continue piling helpers into one place.
2. **One `testutil` per layer (`internal/domain/testutil/`,
   `internal/agent/testutil/`, etc.).** Rejected because it still
   encourages aggregation; a layer's `testutil` will grow to be the
   same kind of dump, just smaller. The `*test` *sub-package* rule
   forces helpers to cluster around the production package they fake,
   which is the natural unit of test cohesion.
3. **Move all helpers into the `*_test.go` files of each consumer
   package (no shared helpers at all).** Rejected because some helpers
   (`MockGateway`, `TestEventBus`) genuinely have many consumers
   (8+ files each), and copy-pasting them is a maintenance hazard.
   The `*test` sub-package is the smallest unit that supports sharing
   without aggregating.
4. **Use a code generator (e.g. `mockgen`) and check in generated
   mocks.** Out of scope for this refactor; can be revisited as a
   separate decision. Generated mocks would still need a destination
   convention, and this ADR's rules would apply.

## Compliance & Enforcement

- `verify_architecture` (the project's layer-rule checker) catches any
  production code importing a `*test` package or `testfixtures`.
- `make verify-testutil-convention` (invoked by `make test`, `make check`,
  and `make check-full`) catches any new file matching
  `internal/.*/testutil/.*\.go` and fails the build with a pointer to
  this ADR.
- `make verify-mock-pattern` (invoked by `make check` and `make check-full`)
  greps for `"github.com/stretchr/testify/mock"` imports in `*test/` packages
  (excluding `agentinternal/`) and fails the build with a pointer to the
  Mock construction pattern section of this ADR.
- Code review checklist: when reviewing a new test helper, verify it
  is placed in `<production-pkg>/<pkg>test/` or `internal/pkg/testfixtures/`,
  not in a centralized helper package.

## References

- `docs/refactor/testutil-audit.md` — the full 8-session refactor record,
  including the symbol inventory, per-session outcomes, and lessons
  learned.
- ADR-017 (Black-Box Integration Test Tree at `tests/`) — establishes
  the *integration*-test layout. This ADR addresses the *helper* layout
  for both unit and integration tests.
- Go standard library: `net/http/httptest`, `testing/iotest`,
  `testing/quick` — precedents for the `<pkg>test` naming convention.
