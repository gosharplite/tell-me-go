# ADR-033: Narrow the `auth.Authenticator` Interface (Remove `getToken`)

- **Status:** Proposed
- **Date:** 2026-05-10
- **Deciders:** @thptcnec (Architect)

## Context

The `auth.Authenticator` interface in `internal/infrastructure/auth/auth.go` contains an unexported method:

```go
type Authenticator interface {
    getToken(ctx context.Context) (string, error)  // unexported
    Invalidate()
    Apply(ctx context.Context, req *Request) error
}
```

Because `getToken` is unexported, **no package outside `auth` can implement `Authenticator`**. This seals consumer packages (gemini, openai, anthropic, llm) out of their own abstraction, forcing them to depend on `auth`'s concrete types (`BearerAuth`, `APIKeyAuth`, etc.) — the root cause of the test-literal coupling identified in issues #298 and #299.

AST-based analysis (`find_usages`) confirms that `getToken` has **zero cross-package callers**: all 22 references are confined to `auth.go` (internal `Apply` dispatch) and `auth_test.go` (same-package tests). `Apply()` is the only method any consumer ever invokes.

Two paths were evaluated:

### Option A — Export `getToken` as `GetToken`

- Rename `getToken` → `GetToken` on the interface and all 6 implementations.
- **Rejected because:** forces every implementation to expose token retrieval as a public API, even though `Apply()` is the only behavior consumers need. Creates 6 new public methods to maintain, and introduces a security concern (consumers could bypass `Apply`'s header-injection logic to leak tokens).

### Option B — Remove `getToken` from the interface

- Interface shrinks to:
  ```go
  type Authenticator interface {
      Invalidate()
      Apply(ctx context.Context, req *Request) error
  }
  ```
- `getToken` remains a private method on each concrete type — no behavior change.
- All 6 concrete types (`VertexAuth`, `APIKeyAuth`, `BearerAuth`, `AnthropicAuth`, `ServiceAccountAuth`, `noOpAuth`) are unaffected: their `Apply()` methods call their own `getToken` via receiver-bound dispatch, not via the interface vtable.

## Decision

**Option B: Remove `getToken` from the `Authenticator` interface.**

The `getToken` method is an internal helper of each concrete type's `Apply()` method. It has no business on the public contract. The narrowed interface honors the Interface Segregation Principle — consumers see only the two methods they actually use (`Apply`, `Invalidate`).

## Consequences

### Positive
- Third-party auth schemes (corporate SSO, mTLS bridge, etc.) can now live in their own packages by implementing the `Authenticator` interface — no dependency on `auth`'s concrete types.
- Consumer packages (gemini, openai, anthropic, llm) can define local test doubles for `Authenticator` without importing `auth.BearerAuth` or any other concrete type.
- Interface surface area shrinks from 3 methods to 2 — both consumed.
- Zero behavior change: `getToken` remains private on all 6 concrete types.

### Negative
- Minimal. The interface contract becomes slightly less self-documenting — one must read a concrete type to understand that `Apply` internally uses a token-fetching helper. Mitigated by the fact that `Apply`'s behavior (inject credentials into request headers) is obvious from its signature and doc comment.

### Neutral
- The `errorAuthenticator` in `provider_health_test.go` (which already implements only `Invalidate` + `Apply`) requires no change.
