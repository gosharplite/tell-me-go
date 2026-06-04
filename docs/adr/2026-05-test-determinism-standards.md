# ADR-036: Test Determinism Standards (No-Sleep, Race-Safe Mocks, Deterministic Time)

## Status
Accepted

## Context

Three consecutive commits revealed an emerging but undocumented set of project-wide testing
standards. Each fixed a distinct class of non-determinism or architectural leakage, and each
would be invisible to a new contributor reading only the existing ADRs:

- **`a6fc9b0f`** — Corrected domain-type leakage: `float64` → `int64` for Task IDs. A
  transport-layer primitive (JSON's default `float64` for numeric fields) had leaked into
  the domain model, making ID comparisons fragile.
- **`347f6c45`** — Eliminated 24 instances of `time.Sleep`-based synchronization in tests.
  These sleeps were the primary source of CI flake, causing timing-dependent failures that
  were impossible to reproduce locally.
- **`fefb99f6`** — Fixed a data race in `MockClock` by adding `sync.Mutex` to guard
  `CalledMethods` and `CalledNow`. The race would only manifest under `go test -race` in
  CI and was invisible to standard `go test` runs.

Without an ADR, these fixes are just commits — future contributors may unknowingly
reintroduce `time.Sleep` into tests, add unguarded mutable state to mocks, or leak
transport-layer types into the domain. The standards codified here close that gap.

## Decision

### 1. No `time.Sleep` for Synchronization

`time.Sleep` is **prohibited** in test code for the purpose of waiting for asynchronous
behavior to complete. The only permitted use is simulating latency inside a fake
implementation, which **must** carry an inline comment:

```go
time.Sleep(10 * time.Millisecond) // Simulating latency: ...
```

The canonical precedent is in `internal/agent/session/doc.go`:

> *"Tests in this package must be 100% deterministic and are forbidden from using
> time.Sleep for synchronization."*

This rule extends to all packages. Any `time.Sleep` in a `*_test.go` file or test helper
that is not accompanied by a `// Simulating latency:` comment is a violation.

### 2. Allowed Deterministic Primitives

The following primitives are the approved replacements for `time.Sleep`-based
synchronization:

| Primitive | Usage |
|-----------|-------|
| `require.Eventually` | Poll observable state with a tick interval and timeout. Suitable when the system under test offers no signaling channel. |
| Channel-based ready/completion signals | Pattern: `startedCh := make(chan struct{})` closed by a mock callback or goroutine on entry; `doneCh := make(chan struct{})` closed on completion. The test blocks on `<-startedCh` or `<-doneCh`. |
| Pre-cancelled contexts | `ctx, cancel := context.WithCancel(context.Background()); cancel()` — preferred over `go func() { time.Sleep(d); cancel() }()`. |
| Test-controlled tick channels | Replace `time.Ticker` with a `chan time.Time` injected via constructor or functional option. The test advances time by sending to the channel. |

**`require.Eventually` example:**

```go
require.Eventually(t, func() bool {
    return service.IsReady()
}, 2*time.Second, 10*time.Millisecond)
```

**Channel-based ready signal example:**

```go
startedCh := make(chan struct{})
mock.On("StartProcessing", mock.Anything).Run(func(args mock.Arguments) {
    close(startedCh)
})
go sut.Run(ctx)
<-startedCh // Blocks deterministically until StartProcessing is called
```

**Pre-cancelled context example:**

```go
// Good: deterministic
ctx, cancel := context.WithCancel(context.Background())
cancel()

// Bad: introduces timing dependency
ctx, cancel := context.WithCancel(context.Background())
go func() {
    time.Sleep(100 * time.Millisecond)
    cancel()
}()
```

**Test-controlled tick channel example:**

```go
tickCh := make(chan time.Time)
fake := &FakeTicker{C: tickCh}
go sut.Monitor(ctx, fake)
tickCh <- time.Now() // Deterministically advances one tick
```

### 3. Race-Safe Mocks/Fakes by Construction

All mock and fake types that track mutable state (call counts, method arguments, captured
values) **must** guard that state with `sync.Mutex`. This ensures tests pass reliably under
`go test -race`.

The canonical example is `MockClock` in `internal/agent/agenttest/mock_clock.go`, which
uses `sync.Mutex` to guard `CalledMethods`, `CalledNow`, and `CurrentTime`. Any new mock
or fake that accumulates state must follow the same pattern.

For positive synchronization (asserting that a mock call occurred), prefer
[ADR-011](2026-04-uibridge-actor-model.md) §3 Rule 2: use `testify/mock`'s `.Run()`
callbacks to signal channels, rather than polling.

### 4. `go test -race` Mandatory in CI and Locally

`make check-full` already runs `make test-race` package-by-package (see `Makefile` lines
357–371). This standard makes running `go test -race` a **hard requirement** before PR
submission. A PR that passes `make check` but not `make check-full` is not ready for
review.

The `test-race` target iterates packages sequentially with `-timeout 60s` to avoid the
timeout issues that occur when running `go test -race ./...` globally on constrained CI
runners.

### 5. Domain Types Must Not Leak Transport-Layer Primitives

JSON's default `float64` for numeric fields must **never** appear in domain types. Domain
types must use the appropriate domain-native type (e.g., `int64` for IDs, `time.Duration`
for intervals) and handle conversion at the transport boundary (HTTP handlers, JSON
codecs, CLI adapters).

Commit `a6fc9b0f` is the motivating fix: Task IDs were typed as `float64` in the domain
because JSON unmarshalling produced that type by default, and the conversion was never
applied at the boundary. This made ID equality comparisons unreliable (`1.0 != 1` in some
contexts) and leaked a serialization concern into pure domain logic.

## Cross-References

- [ADR-011](2026-04-uibridge-actor-model.md) — Defines the original deterministic testing
  rules (Rules 1–3) that this ADR generalizes across the project.
- [ADR-022](2026-04-test-doubles-in-pkgtest-subpackages.md) — establishes the
  `<pkg>test` mock construction pattern. The encapsulated spy variant in ADR-022
  conforms to this ADR's §3 race-safety requirement.
- [ADR-032](2026-05-test-hook-policy.md) — Defines the test-hook policy that provides the
  legitimate exception mechanism for unexported concrete types where interface mocking is
  impossible.

## Consequences

- **Positive**: Durable elimination of CI flake caused by `time.Sleep`-based tests and
  data races in mocks. Each standard addresses a real, observed failure mode.
- **Positive**: Clear onboarding path — new contributors can read this ADR and understand
  _why_ certain patterns are forbidden and _what_ to use instead.
- **Negative**: Slightly more upfront test design effort. Writing a channel-based
  synchronization point or injecting a tick channel takes more lines than a `time.Sleep`
  call. Accepted as a necessary trade-off: the cost of debugging a flaky CI failure far
  exceeds the cost of writing a deterministic test.

## Compliance

`make check-full` is the enforcement gate. It runs the full quality pipeline including
`test-race`, `lint` (via `golangci-lint`), and the architecture verification suite. Any
PR that does not pass `make check-full` is blocked from merge.

### Automated Gate: `verify-no-test-sleep`

The `verify-no-test-sleep` Makefile target (invoked by both `make check` and
`make check-full`) enforces the no-`time.Sleep` rule mechanically via `grep`:

- **Zero tolerance** for `internal/ui/` — any `time.Sleep` in UI test files is a hard
  violation with no allow-list.
- **`config/watcher_test.go`** — allowed only when the sleep is accompanied by a
  `// simulates I/O latency` inline comment.
- **`telemetry/system_metrics_darwin_test.go`** — allowed as a file-level exception for
  Darwin kernel tick observation (hardware sampling, not synchronization).
- Cross-platform: POSIX (shell) and Windows (PowerShell) implementations both gate on
  the same rules.

Any other `time.Sleep` in a `*_test.go` file fails the build. New allow-list entries
require an amendment to this ADR.
