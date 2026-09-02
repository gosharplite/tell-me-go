# ADR-055: Tools-Layer Filesystem Access via Injected Domain Port (defaultFS Fallback)

**Status:** Accepted
**Date:** 2026-08-08
**Related:** [Issue #1295](https://github.com/gosharplite/tell-me-go/issues/1295) (PR1: analysis transaction port-routing; PR2: workspace convention + import gate), [ADR-034](2026-05-cosmetic-lint-fix-pr-separation.md) (cited for sequencing only), `docs/sop/technical/architecture_and_packages.md`

## Context

Two production files in `internal/tools/` — `internal/tools/analysis/refactor.go` and `internal/tools/workspace/process_executor.go` — were the only production files in the tools layer importing `internal/infrastructure/persistence`. Each constructed `infra_persistence.NewOSFileSystem()` directly, building an infrastructure adapter inside the tools (application use-case) layer.

The corrected framing: the DI thread already exists end-to-end. `ToolRegistrationParams.FileSystem` (`internal/tools/tools.go`) is populated in `internal/infrastructure/di/toolchain_factory.go` (`FileSystem: infra_persistence.NewDomainFS(f.FileSystem)`) and passed into `workspace.Register` and `analysis.Register`. The real defect was **two dropped parameters**:

- `analysis/manager.go` → `newRefactorManager(sp)` dropped `fs` → `newTransaction()` self-constructed the adapter.
- `workspace/registration.go` → `registerSystem(...)` (no `fs` param) → `newshellTool(...)` → `newprocessExecutor()` self-constructed the adapter.

This is a package-boundary refactoring (ADR trigger #2: Domain vs. Infrastructure) that introduces a new design pattern (ADR trigger #3): the injection convention with a named `defaultFS` fallback.

**ADR-034 is cited for sequencing only.** Its "cosmetic" definition does NOT cover this change — the dependency graph is altered.

## Decision

### 1. Injection convention + `defaultFS` fallback + seam panic

Every live tool path uses the injected domain port (`persistence.FileSystem`). Direct adapter construction in tools is confined to exactly one sanctioned `default_fs.go` fallback per package (`internal/tools/analysis/default_fs.go`, `internal/tools/workspace/default_fs.go`), holding `var defaultFS = infra_persistence.NewOSFileSystem()`. The zero-arg `newTransaction()` remains and delegates to `defaultFS`; the injected path goes through `newTransactionWithFS(fs)`, which panics on nil with an actionable message ("use newTransaction() for the default") — a contract-violation guard on a test-reachable seam (in production the hub guarantees non-nil). An import-based gate (`verify-tools-adapter-import`, landing with PR2) enforces that no other tools production file imports `internal/infrastructure/persistence`.

### 2. `Commit` → `AtomicWrite` semantics

`transaction.Commit` (analysis) now buffers each formatted file and writes via `tx.fs.AtomicWrite(ctx, path, buf.Bytes(), 0644)` instead of hand-rolling a fixed-name `.tmp` + `os.Rename` sequence. This changes write semantics on a core path:

- **Permissions tighten from `0666` to `0644`** (regression-visible; deliberate). `0644` matches the existing tools precedent (`workspace/backup.go`, `integrations/media.go`).
- **fsync**: `AtomicWrite` syncs the temp file before rename (guards against zero-byte files on power loss).
- **Retry**: rename retries up to 5 attempts with linear backoff on transient failures.
- **EXDEV**: cross-device rename falls back to copy + remove.
- **Windows transient-rename handling**: "Access is denied" / "used by another process" (non-directory target) is retried instead of failing `rename_symbol` / `move_definition`.
- **Context cancellation** aborts mid-commit (checked before write, before sync, and in the retry loop) instead of writing against a cancelled context.
- Cross-file partial-commit semantics are unchanged: per-file atomic writes; earlier files stay committed on a mid-commit failure (same as before).

### 3. `LoadFile` port-routing

`transaction.LoadFile(ctx, path)` reads via `tx.fs.ReadFile(ctx, path)` and parses the returned bytes — completing the transaction's read path through the port.

### 4. Non-goals

- `loadGoFilesForRename`'s `filepath.Glob` discovery stays OS-bound — the domain port has no `Glob`; a `ReadDir`+matcher is new code, out of scope. Memory-FS testability covers the transaction's read/write paths only.
- Shape B (full port-routing of the `processExecutor` write path in workspace) is rejected — out of scope, separate ADR'd issue if ever wanted.
- Swap-validation desync: the write path is swappable; validation remains OS-bound by design.

## Consequences

- **Positive**: the transaction is unit-testable against an in-memory filesystem (`persistence.NewMockFileSystem`) — a proof-property test (`TestTransaction_MemoryFS_LoadTransformCommit`) fails against pre-change code and passes post-change; Windows `rename_symbol`/`move_definition` no longer fail on transient rename errors; context cancellation is honored mid-commit; adapter construction in tools is confined to one named fallback per package, mechanically gated.
- **Regression-visible**: file permissions written by the analysis transaction tighten from umask-dependent `0666` to exact `0644` — deliberate, documented here.
- **Memory profile**: the whole formatted file is buffered in memory instead of streaming into an fd — non-issue for Go sources (`format.Node` already holds the AST).
- **Factual note on workspace**: the workspace changes (PR2) are **zero-runtime-delta by design** — the value is the boundary rule and its gate, not behavior.
- **SafePath**: injection makes `fs` a zero-friction registered dependency in `ToolRegistrationParams`; SafePath-before-write is enforced by convention and domain-model invariants, not by the injection mechanism. No new unvalidated write path is created — validation precedes FS use in both paths today and remains.
- **Plugin contract**: unchanged. `plugin.go` already ships `PluginDependencies.FileSystem persistence.FileSystem`; third-party plugins live outside the module and the gate cannot see them.

## Related

- `docs/sop/technical/architecture_and_packages.md` — tools-layer boundary rule (see Step 3)
- [ADR-034](2026-05-cosmetic-lint-fix-pr-separation.md) — sequencing reference only
- **Amended 2026-09 (ADR-074):** the workspace `defaultFS` fallback is retired with the zero-arg `newprocessExecutor()`; `verify-tools-adapter-import`'s sanctioned list shrinks to `internal/tools/analysis/default_fs.go` only — the defaultFS rationale (dozens of consumers) does not transfer to a one-consumer runner chain. See [ADR-074](2026-09-process-runner-injection.md).
- **Amended 2026-09 (#1465):** the analysis `defaultFS` fallback is retired with the zero-arg `newTransaction()` — its only remaining callers were tests (8 `refactor_test.go` sites + 11 `refactorManager` test constructions), and production paths were already injected (`newTransactionWithFS(m.fs)` at `refactor_manager.go`). `verify-tools-adapter-import` loses its last sanctioned-file exclusion and becomes a pure no-infrastructure-imports assertion; the `verify-tools-infrastructure-import` (ADR-062) exclusion is removed in the same change. Precedent: the ADR-074 workspace retirement (#1460). No `IntentionalNonFix` entry is added — nothing remains to catalog. See [ADR-074](2026-09-process-runner-injection.md).
- **Amended 2026-09 (#1469):** the last two non-DI adapter-construction fallbacks are retired — the auth `defaultFS` package-level `NewOSFileSystem()` (`internal/infrastructure/auth/default_fs.go`, deleted) with the zero-arg `NewVertexAuth()` becoming `NewVertexAuth(fs)`, and the history `defaultAssetStore` wrapper (`internal/infrastructure/history/default_asset_store.go`, deleted) with the test-only `NewManager` gone; every `NewManager` test call site now uses `NewManagerWithAssetStore` with an AssetStore built by the test-side helper `persistencetest.NewAssetStore`. `NewOSFileSystem`/`NewAssetStore` are now constructed only in `internal/infrastructure/di` + test-double packages. Same remediation pattern as the #1465 analysis retirement. No `IntentionalNonFix` entry is added — nothing remains to catalog.
