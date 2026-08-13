# ADR-065: Infra-Only Multi-Package Seam Realigns to Concept-Side Contract Leaf

- **Status:** Accepted
- **Date:** 2026-08
- **Deciders:** @thptcnec (Architect)

## Context

`verify-exit-query` (ADR-056 Decision 1, report-only) flagged `ports.Authenticator`
as a NEW exit candidate: it is single-layer infrastructure (consumers = `llm` +
`llm/{anthropic,gemini,openai}`; implementers = `auth`), even when `di` is counted.
It was introduced by commit `e3d4efb6` (#1350 item 4), which promoted
`Authenticator` + `Request` out of `internal/infrastructure/auth` into
`internal/domain/ports`.

The #1357 grill round established three corrections:
1. Option A ("record a stay") is a rule amendment, not a stay — the six existing
   `exitStayRationales` entries all fail the exit test (cross-layer once `di` is
   counted); `Authenticator` passes it.
2. Option B's destination (`back to internal/infrastructure/auth`) is an ADR-003
   Rule #1 violation — that is the implementer package, not the consumer.
3. The `llm/authenticator` leaf is also wrong — it would create an unsanctioned
   `auth → llm` cross-family infra edge (strict-gate failure) and invert ownership.

## Decision

Realign to a contract leaf owned by the `auth` (concept-side) family:
`internal/infrastructure/auth/contract`. This satisfies all four constraints
simultaneously — ADR-056 exit (leaves the domain hub), ADR-003 Rule #1 (contract
out of the implementer), the strict transitive gate (zero new
whitelist/`infra-sanctioned` decisions — reuses the existing `llm → auth` edge),
and the import cycle (a leaf under `auth`, not `llm`).

Rename the data carrier `Request` → `AuthHeaders` as a named map (value
parameter), because `Request` is a live `http.Request`-collision hazard (already
visible in `buildRequest(ctx, authReq *ports.Request) (*http.Request, error)`).

Final contract:

```go
package contract

type AuthHeaders map[string]string

type Authenticator interface {
	Invalidate()
	Apply(ctx context.Context, headers AuthHeaders) error
}
```

### The reusable rule

This ADR codifies a rule that fires only when ALL preconditions hold:

1. ADR-056 Decision 1 exit fires (single-layer including `di`).
2. Consumers span ≥ 2 packages and no single consumer package can host the
   contract (split consumer + import cycle).
3. Implementer family ≠ consumer family.
4. A sanctioned (or ratifiable) `consumer → concept` lateral edge exists.
5. The seam's concept is unambiguously owned by one family (here `auth` =
   authentication).

**Invariant:** an infra-only multi-package seam meeting these preconditions
realigns to a contract leaf in the concept-side family and is never re-promoted
to `ports`.

### Amendment to ADR-056 Decision 1

ADR-056 Decision 1's destination clause ("the seam moves to that layer's
package") is amended for the infra-only multi-package seam case: the seam moves
to a **contract leaf in the concept-side family** (not the implementer package,
not the consumer package).

### Reconciliation of ADR-003 Rule #1

ADR-003 Rule #1 ("interfaces defined in the consuming package") does not apply
to an infra-only multi-package seam with a split consumer and an import cycle —
there is no single consumer package that can own the contract. The concept-side
contract leaf is the reconciled home.

## Consequences

**Positive:**
- `ports.Authenticator` and `ports.Request` are deleted from the domain hub;
  `verify-ports-registry` family count drops 9 → 8.
- The `http.Request`-collision hazard is eliminated by the `AuthHeaders` rename.
- Edge-neutral for the transitive gate: the only new infra edges are the
  already-sanctioned `llm → auth` edge (`auth/contract` is self-justifying — it
  imports only `context`).

**Negative:**
- None material. Consumers (`llm` + provider subpackages) now import the leaf
  via the aliased `authcontract` package.

## Rejected alternatives

- **Option A (stay)** — a rule amendment, not a stay; violates
  `exitStayRationales`' documented contract.
- **`llm/authenticator` leaf** — unsanctioned `auth → llm` edge + ownership
  inversion.
- **`Credentials` name** — implies secret material (input); the carrier holds
  output headers.
- **`Request` kept** — `http.Request`-collision hazard.
- **Struct wrapper `AuthHeaders{Values: …}`** — redundant indirection for zero
  benefit (YAGNI).
