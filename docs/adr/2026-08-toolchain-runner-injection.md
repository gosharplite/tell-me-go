# ADR-060: Inject the Go Toolchain Runner into the Tools Layer (ToolchainRunner Domain Port)

**Status:** Accepted
**Date:** 2026-08
**Related:** [Issue #1325](https://github.com/gosharplite/tell-me-go/issues/1325), [ADR-055](2026-08-tools-filesystem-injection.md), [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md), [ADR-003](2026-01-domain-decomposition-strategy.md), [ADR-021](2026-04-test-doubles-in-pkgtest-subpackages.md), [ADR-017](2026-04-blackbox-integration-test-tree.md)

## Context

Five production files in `internal/tools/` leaked the `internal/infrastructure/toolchain` adapter across the tools layer, plus a second leaked symbol:

- Two direct constructions: `toolchain.NewGoRunner(executor)` at `internal/tools/analysis/manager.go:81` (inside `newAnalysisManager`) and `toolchain.NewGoRunner(exec)` at `internal/tools/developer/registration.go:19`.
- Three forced type leaks: `AnalysisGoRunner` (`manager.go:55`), the developer `goRunner` (`dev.go:32`), and `releaseGoRunner` (`release.go:27`) all returned `toolchain.CoverageReport`; `dev.go:221` (`var report toolchain.CoverageReport`) was the fifth touchpoint.
- A second leaked symbol: `toolchain.ErrNoSupportedLinter`, consumed at `analysis/health.go` (`errors.Is(err, toolchain.ErrNoSupportedLinter)` in `runLint`).

The edge was **invisible to both architectural gates**: the layer gate's `layerTools` rule forbids only `application` and `cmd` (infrastructure is not forbidden), and ADR-056's `familyOf()` returns `""` outside `internal/domain/`, so no whitelist row can express a tools→infrastructure edge. The edge passed CI not because it was adjudicated but because the instruments cannot see it — the same class ADR-055 addressed with a purpose-built grep gate.

## Decision

### 1. ADR-055-extension framing (not a naked ADR-003 Rule #1 deviation)

`ToolchainRunner` homes in `internal/domain/tools` as an **extension of the ADR-055 precedent to the toolchain family**. ADR-055 ratified this exact seam shape one month earlier: a domain port (`persistence.FileSystem`), consumed by `internal/tools`, implemented by `internal/infrastructure/persistence`, concrete confined under a grep gate. The `ToolchainRunner` seam is the identical consumer/implementer pair applied to the Go toolchain. ADR-003 Rule #1's *intent* is satisfied — the consumer family defines the contract; the implementer does not dictate it; only the physical-location letter is superseded, consistent with ADR-055/ADR-056. Gate mechanics are the **enforcement consequence, not the primary reason**: a home in `internal/tools` would force `infrastructure/toolchain → internal/tools`, ballooning the implementer's closure to the whole tools tree; a new `internal/domain/toolchain` would add a tenth domain family to `derivedPortsFamilies` and split `CommandExecutor` from its consumer family; `domain/tools` is the only zero-closure-delta home and sits next to `CommandExecutor` at `internal/domain/tools/executor.go:12`.

### 2. ADR-056 Decision 1 inapplicability

ADR-056 Decision 1 (cross-layer contracts spanning non-importable layers) **never fires** for this seam: `tools` and `infrastructure` are importable layers (the layer rule forbids `layerTools` from importing only `application` and `cmd`; `layerInfrastructure` from importing only `application` and `cmd`). The ADR-055 precedent, not ADR-056, governs this seam.

### 3. The three-home analysis

- `internal/tools` home: would invert the dependency — `infrastructure/toolchain` would import the whole tools subpackage tree (a decision-required closure payer).
- New `internal/domain/toolchain` family: adds a tenth family to `derivedPortsFamilies` and splits `CommandExecutor` from its consumer family.
- `internal/domain/tools`: zero closure delta — `infra/toolchain` already imports `domain/tools` (for `CommandExecutor`), the tools production files already import `domain/tools`, and the di composition root already imports `infra_toolchain`. The graph delta of PR1 is purely the deletion of the five tools→infra edges plus the single di construction.

### 4. Corrected infra/toolchain import surface + ports-hub derivation

`internal/infrastructure/toolchain`'s module-internal import surface is **`{domain/ports, domain/tools}`**, not `{domain/tools}` alone: `toolchain_health.go` imports `domain/ports` (it implements `ports.HealthChecker` for `BuildHealthChecker`); `runner.go` imports `domain/tools` (for `CommandExecutor`, and post-PR1 the `ToolchainRunner` port). The implementer remains **self-justifying** under the ratified ADR-056 gate semantics: the BFS reaches `{toolchain, ports, tools}`; `collectReachableNodes` marks `ports` visited-but-not-expanded (hub rule) and `extractDomainFamilies` never attributes the ports family; `domain/tools` is a leaf family. Hence `Dom(infra/toolchain) = {tools} = direct` — approved-constant, no whitelist row before or after. The classification is contingent on the hub semantics; the false "only import is domain/tools" claim from grill round 1 is corrected here.

### 5. Tool-boundary designed-nil guard criterion (verbatim) + per-field citations

A `ToolRegistrationParams` field is **guarded** unless the tools layer defines a designed nil-mode *at the tool boundary*, evidenced by exactly one of: (i) a registration-time skip — the tool is absent from the registry when the field is nil; (ii) a tool-handler return of a sentinel degrade (`ErrNotImplemented`) on nil; (iii) a documented default-substitution in the tool's construction. An internal nil-check inside a private consumer helper is **not** a tool-boundary mode and does not exempt the field.

Guards (8): `Registry`, `SecurityManager`, `WorkspacePolicy`, `FileSystem` (unconditional DI contract); `CommandExecutor` (no nil-mode anywhere; panics at `gitManager.Exec.Output`); `CommandValidator` (`shell.go:163` `if w.validator != nil && w.validator.HasBareNewline(command)` is a private Windows capability probe — internal-fallback non-exemption; the core contract `validateTestCommand → m.validator.Split` has no nil mode and panics); `EventBus` (`info.go:212` `if m.Events == nil` is a private side-effect skip — non-exemption; the dev/workspace publication contract — `dev.go:357`, `shell.go:467,471,479,543` raw `Publish` — has no nil mode and panics); `ToolchainRunner` (`health.go:272` `if m.Runner != nil` is a private repo-root fallback — non-exemption; every core port method panics on a nil interface).

Exclusions (3): `SessionProvider` (registration skip, `tools.go` `if params.SessionProvider != nil`); `HealthManager` (registration skip, `workspace/registration.go:392` `if health != nil`); `Client` (sentinel degrade `media.go:60,232` `ErrNotImplemented`; default substitution `network.go:36`, `teams.go:31`, `atlassian/jira.go:30` `&http.Client{Timeout: 30s}`; `integrations/registration.go` never populates `HTTPClient`).

`TestRegisterAll_Errors` gained exactly 4 rows (CommandExecutor, CommandValidator, EventBus, ToolchainRunner) — not 5 (the Client row was retracted in grill Q2).

### 6. Drift status of PR1 symbols + sentinel contract + declaration-style pin

Traced against `scripts/modelith-drift.sh` against the code model:

| Symbol | Drift status |
| --- | --- |
| `ToolchainRunner` | **Escapes** — only via the `Tool`-substring false-negative (the entity-substring loop matches `"ToolchainRunner"` against the modeled entity `Tool`). An accidental escape, not model coverage; the gate is too weak to be decision-relevant. |
| `CoverageSummary` | **TRIPS** — accepted advisory; the DTO is a port data shape like `CommandExecutor`/`ToolResult`/`Schema`/`Registry` (all in `domain/tools`, none modeled). No model edit. |
| `ErrNoSupportedLinter` | **Escapes** — deterministic via the pinned single-line declaration `var ErrNoSupportedLinter = errors.New(...)` (a `var (...)` block would trip). The pin is part of the sentinel contract. |

The DTO choice rests on the verified 4-field consumer union (`PassedCount`, `NoGoFiles`, `CoveragePct`, `SummaryOutput` consumed at `dev.go` `getCoverage` and `health.go` `runTestsAndCoverage`) and `TestOutput`'s zero assertion consumers — not on drift (both DTO and report trip identically). Sentinel contract: couplings to *our* wording are typed (`errors.Is`); couplings to stdlib wording (`"exit status 1"`) are documented, not converted; semantics are "detection; severity consumer-decided" (health SKIP vs release FAIL).

### 7. Per-destination wiring-test contract

Six runner-consuming fields receive the `ToolchainRunner`: `dependencyAnalyzer.Runner`, `infoManager.Runner`, `architectureManager.Runner`, `healthManager.Runner` (assigned inside `newAnalysisManager`), `devManager.runner`, `releaseManager.runner` (assigned inside `developer.Register`). Every field must have either a behavioral probe asserting the injected fake was reached (identity + exact method) or a white-box construction assertion. Assignment is proven by the wiring tests; the 12-method *behavioral* surface is proven by the infra adapter's own tests (`runner_test.go`) — neither surface covers the other. A dropped assignment compiles (nil satisfies the interface), so per-destination coverage is the enforcement, not review.

### 8. toolstest fake home + stub accounting

One canonical `toolstest.FakeToolchainRunner` (ADR-021 locality — the common ancestor of the three consumer sub-trees; ADR-056 canonical-mock-home; coverage-excluded). Stub accounting: **6 exercised** by probes — `GetGoDoc` (go_doc), `RunTestsWithCoverage` (get_coverage), `RunLinter`/`RunTests`/`BuildCode` (verify_release_readiness), `CheckGovulncheck` (seam capture); **6 compile-time-only** — `GetPackageList`, `GetModulePath`, `GetModuleDir`, `RunBenchmarks`, `RunModTidy`, `FormatCode`. All 12 record to the call log, so a probe that unexpectedly reaches the wrong method fails its identity assertion with the recorded call in hand.

### 9. Surviving test-layer construction census + anti-extension decision + the promise's three conditions

22 test-file `toolchain.NewGoRunner(...)` constructions survive by design across 4 files: `coverage_parser_test.go` ×17, `health_test.go` ×2, `real_nonfix_catalog_test.go` ×2, `architecture_bench_test.go` ×1. They are the **real-adapter-over-mock-executor verification surface** — the actual parsing/consumption paths fed with realistic `go test`/`go tool cover -func=` output through the real runner — which is precisely the substitute the e2e deferral names. **Anti-extension decision**: `verify-tools-toolchain-import` must never be extended to `_test.go` files; doing so would break the 22 sites and destroy the verification the deferral depends on. The "future go-tooling tools copy this pattern without fresh adjudication" promise has three conditions: (1) consumer side — `domain/tools` port, single di construction, production-file confinement gate; (2) implementer side — domain imports stay within the ports hub plus the consumer family, keeping the implementer self-justifying; (3) test layer — direct adapter construction remains legitimate in tools `_test.go` files per ADR-021 and the issue's own "test files may continue importing" language; the gate is not extended to test files.

### 10. e2e known-gap + seam-capture probe rationale

The real-adapter e2e boundary at the `di/toolchain_factory.go` construction line is deliberately deferred. Trigger, verbatim from the maintainer decision: **"Fund real-adapter e2e in the blackbox integration tree (`tests/integration/`, per ADR-017) when a Go toolchain output-format change breaks the parsers in the field** — i.e., when `go test` / `go tool cover -func=` output drift is reported (or a Go release notes change in test/cover output formats) rather than preemptively."

The unit-level verification is the **seam-capture probe** (`TestBuildRegistry_PopulatesToolchainRunner`): capture `ToolRegistrationParams` via the injectable `RegisterAllTools`, assert `ToolchainRunner` non-nil, then call `CheckGovulncheck` directly on the captured port (lookPath-only, no subprocess). Handler-level probing is forbidden: `check_vulnerabilities` falls through to a real `govulncheck ./...` via the non-injectable `devManager.executor` (dev.go), and `make check` guarantees govulncheck is installed — a handler-level probe would spawn a real subprocess and be environment-dependent, contradicting the deferral rationale.

### 11. Follow-ups

- `verify-tools-adapter-import` predicate harmonization: align its predicate to the same non-`_test.go` + `*test/`-path-exclusion form as `verify-tools-toolchain-import` when next touched.
- Five raw `eventBus.Publish` sites (`dev.go:357`, `shell.go:467,471,479,543`): nil-guard vs. `SafePublish` conversion (the latter adds a 2s-timeout wrapper nuance) — architect's choice, documented follow-up.
- `devManager.executor` non-injectability: a `withExecutor` devOption would enable handler-level tests later; recorded residual, out of #1325 scope.

## Consequences

- **Positive**: single runner construction at the composition root; direct-construction class eliminated from production and mechanically gated; per-destination wiring proven by tests; zero mock growth (narrow consumer interfaces survive per ADR-003 Rule #2 and the MockEstimator ruling).
- **Regression-visible**: none — PR1 is zero-runtime-delta by design; the value is the boundary rule, its gate, and its verification.
- **Accepted advisory**: `CoverageSummary` trips `modelith-drift`; no model edit (port data shape, consistent with `CommandExecutor`).
- **Known-gap**: real-adapter e2e deferred with the trigger above.
- **Issue-closure mapping**: the "clean up the ADR-056 whitelist" criterion is satisfied as a documented non-change — no whitelist row ever existed for the edge (the gate's unit of measurement has no slot for an infrastructure edge); the ADR/index criterion is satisfied by this record plus `verify-adr-index`.
