# Intentional Non-Fixes

Items evaluated by the architect and deliberately NOT pursued.
Any AI agent recommending these should consult the rationale below.

---

## Coverage Gaps (ACCEPTED)

### pidlock/pidlock.go — 6 gaps

- **Status**: ACCEPTED (2026-06-28, commit `0f882423`)
- **Rationale**:
  - **2 gaps are platform-specific**: the Windows `return true` branch in
    `isProcessAlive` and the `IsStale` `os.Stat`-failure fallback are
    unreachable in `pidlock_unix_test.go` (the only test file).
  - **2 gaps are structurally unreachable on Linux**: `os.FindProcess`
    never fails on Linux (it wraps the PID without a syscall); the ESRCH
    branch is tested by `TestIsProcessAlive_DeadProcess` but the coverage
    tool reports it as uncovered when the dead-PID test Skips.
  - **2 gaps require filesystem fault injection**: `fmt.Fprintf` and
    `f.Sync()` to a just-opened `*os.File` can only fail on disk-full or
    hardware error. Not worth the test complexity for a process-lock helper.
- **See**: `internal/infrastructure/pidlock/pidlock.go` (architect-acceptance
  comments at each gap site)

### history/global_prompt_tracker.go — writeCompactedData branch

- **Status**: ACCEPTED (2026-06-28, commit `0f882423`)
- **Rationale**: `writeCompactedData` cannot return false when writing to
  `bytes.Buffer`: `json.Marshal` on `promptEntry` never fails (all string
  fields), and `bytes.Buffer.Write` never returns an error. Structurally
  unreachable.
- **See**: `internal/infrastructure/history/global_prompt_tracker.go`
  (architect-acceptance comment at the `prepareCompactedEntries` call site)

---

## ADR-021 Follow-Ups (ALL COMPLETE)

### verify-testutil-convention CI lint

- **Status**: DONE
- **Detail**: `verify-testutil-convention` target in the Makefile, wired into
  `make test`. Fails the build if any `internal/.*/testutil/.*\.go` path or
  `internal/.*/testutil` import is found.

### testify/mock migration

- **Status**: DONE (2026-06-28, commit `505d6b26`)
- **Detail**: `verify-mock-pattern` target in the Makefile enforces **zero
  tolerance**. The single remaining `testify/mock` import in
  `internal/agent/agentinternal/accessor_test.go` is the ADR-021 escape
  hatch — intentionally excluded from the CI check.

### agentinternal/ unused symbols audit

- **Status**: DONE (2026-06-28)
- **Detail**: All 3 exported symbols have active consumers:
  - `AsAgentInternal` — used in `tests/integration/agent/agent_integration_test.go`
    and `tests/integration/agent/regression_test.go`
  - `MockSessionLifecycleManager` — used in `internal/agent/service_test.go`
  - `NewBareAgent` — used in `internal/agent/agent_error_test.go`

### MockEstimator collapse into MockTokenCounter

- **Status**: REJECTED by architect (Session 5, 2026-04-19)
- **Rationale**: Preserved for cohesion. `MockEstimator` satisfies the
  `tokenEstimator` interface (single-method: `EstimateTokens`) while
  `MockTokenCounter` satisfies the broader `TokenCounter` interface.
  Collapsing would force `MockTokenCounter` consumers to depend on a wider
  interface surface. The 2 reference sites in `agent/session/context` are
  stable.
- **See**: `docs/refactor/testutil-audit.md`, Session 5 outcome notes

---

## Design Rejections

### MockEventBusFail fold-into-MockEventBus

- **Status**: REJECTED by architect (Session 4, 2026-04-19)
- **Rationale**: `MockEventBusFail` is `MockEventBus` + an injectable
  `PublishErr` field. The architect chose to preserve both as separate
  types rather than adding an error-injection field to `MockEventBus`,
  keeping the happy-path mock simple.
- **See**: `docs/refactor/testutil-audit.md`, Session 4 outcome notes

### WarmImplementations(ctx) Opt-In

- **Status**: REJECTED ("Won't Do", ADR-039, finalized 2026-06)
- **Rationale**: No consumer has demonstrated measurable UX impact from the
  5-second TTL sawtooth. Background goroutine lifecycle management adds
  complexity disproportionate to the benefit.
- **See**: `docs/adr/2026-05-lazy-implementation-index.md`, "Rejected:
  WarmImplementations(ctx) Opt-In (Won't Do)" section

---

*Last Updated: 2026-06-28*
