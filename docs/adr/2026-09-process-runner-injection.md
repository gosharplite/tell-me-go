# ADR-074: Inject the Process Runner into the Tools Layer (ProcessRunner Domain Port)

**Status:** Accepted
**Date:** 2026-09
**Related:** [Issue #1460](https://github.com/gosharplite/tell-me-go/issues/1460), [Issue #1459](https://github.com/gosharplite/tell-me-go/issues/1459) (superseded by #1460), [ADR-060](2026-08-toolchain-runner-injection.md), [ADR-055](2026-08-tools-filesystem-injection.md), [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md), [ADR-003](2026-01-domain-decomposition-strategy.md), [ADR-021](2026-04-test-doubles-in-pkgtest-subpackages.md)

## Context

Roughly 40 `fault-injection-required` catalog sites in the workspace cluster live behind raw `os/exec` in a single package family, `internal/tools/workspace` (per `docs/architect/INTENTIONAL_NON_FIXES.md`, #1431 batch — grill-verified):

| File | Sites | Branches |
| --- | --- | --- |
| `internal/tools/workspace/process_executor.go` | 14 | `90-92, 98-100, 107-109, 241-243, 246-248, 256-260, 265-267, 269, 472-474, 474-476, 477-479, 484-486, 486-488, 489-491` — Start/Wait/pipe lifecycle in `RunCommand`/`RunPipeline`/`Output`/`CombinedOutput` |
| `internal/tools/workspace/shell.go` | 14 | `259-271, 273-277, 364-376, 378-382, 466-485, 495, 538-541` — execution result/feedback paths in `ExecuteCommand`, `PipeCommands`, `runWithFeedback` |
| `internal/tools/workspace/pipeline.go` | 5 | `122-124, 161-164, 174-175, 175-177, 185` — `pipeline.start`/`wait`/`capture` |
| `internal/tools/workspace/proc_posix.go` | 5 | `35-37, 45-47, 47-49, 52-54` — process-group kill `Cancel` paths |
| `internal/tools/workspace/reader.go` | 2 | `310-312, 344-345` — `getFileDiff`'s `LookPath`+`RunCommand` via the internal `commandExecutor` downcast |
| **Total** | **~40** | |

`fault-injection-required` is the catalog's largest still-growing acceptance class (55 sites from the #1431 batch alone); ~40 of them — roughly 70% — sit in this cluster, all behind one missing abstraction: there is no seam between "assemble the command" and "the OS runs it" (`exec.CommandContext` at `process_executor.go:135` and `pipeline.go:59`; raw `cmd.Start()`/`cmd.Wait()`/`StdoutPipe()`/`StderrPipe()`; `pipeline.wirePipes`' fd hand-off). Every attempt to test these branches against the real subprocess boundary requires OS-level fault injection, so each lands in the catalog and the class grows mechanically with every new workspace branch.

The design was pressure-tested in the **#1459 grill round** (2026-08-30, Architect vs Griller, 5 questions, verdict: **PROCEED WITH CHANGES** — seven substantive corrections folded in) and finalized as **issue #1460**, the grill-converged spec that **supersedes #1459**'s draft design. The port shape in Decision 2 is architect-signed-off per that round's post-grill position; this ADR is the remaining formal precondition for implementation.

One structural fact the grill surfaced: the only existing execution port, `tools.CommandExecutor` (`internal/domain/tools/executor.go`), abstracts execution *for consumers* (`Output`/`CombinedOutput`/`LookPath`), not the process lifecycle *inside* the executor. DI passes an infrastructure executor as `tools.CommandExecutor`, so the `registerFiles` downcast — `exec.(*processExecutor)` at `registration.go:43` (block `registration.go:40-45`) — fails in production, leaving `fileReader.executor` nil behind the designed degrade at `reader.go:305-307` (`validateDiffPrerequisites`' "no command executor available for diffing"). That degrade is **untouched here (non-goal)**: this refactor removes the raw-lifecycle defect behind the workspace cluster's catalog tax, not the reader's optional-executor seam.

## Decision

### 1. Port home: `internal/domain/tools/process.go`

Three-home analysis mirroring ADR-060 §3:

- `internal/tools` home: would invert the dependency — the infrastructure process adapter would import the whole tools subpackage tree (a decision-required closure payer). Rejected.
- New `internal/domain/process` family: adds a family to `derivedPortsFamilies` and splits `ProcessRunner` from its consumer family. Rejected.
- `internal/domain/ports` as a registry member: a hard **no** — `ProcessRunner` is a tools-family consumer port, and adding a row to the ADR-064 registry (`internal/domain/ports/doc.go`) would consume registry capacity for no liveness benefit. The registry is untouched: the bijection and the N≤12 family bound stay green by not appearing in the gate's input at all.
- `internal/domain/tools`: zero closure delta — `ProcessRunner` sits next to `CommandExecutor` (`internal/domain/tools/executor.go`); the infrastructure process adapter imports `domain/tools` for the port; tools production files already import `domain/tools`; the di composition root already imports infrastructure packages. The graph delta is the deletion of workspace's raw-exec lifecycle edges plus a single di construction.

ADR-056 Decision 1 (cross-layer contracts spanning non-importable layers) **never fires** for this seam: `tools` and `infrastructure` are importable layers — the identical ruling as ADR-060 §2. The ADR-055/ADR-060 injection precedent, not ADR-056, governs this seam.

### 2. Port shape: zero dead members (ADR-003 Rule #2)

```go
// Start assembles and starts the process; the adapter applies the platform
// process-group cancellation contract (kill the whole tree on ctx cancel).
type ProcessRunner interface {
    Start(ctx context.Context, spec ProcessSpec) (ProcessHandle, error)
}

type ProcessSpec struct {
    Name  string
    Args  []string
    Stdin io.Reader          // nil = no stdin (today's behavior)
    Env   map[string]string  // overlay map; nil = inherit os.Environ() untouched
}

type ProcessHandle interface {
    Stdout() io.ReadCloser // valid only after the owning Start returns
    Stderr() io.ReadCloser
    Wait() error           // exit-status failures return *tools.ExitError
}

// Domain-typed, errors.As-able, code-carrying. Code -1 = signal-killed
// (today's observable behavior, made fake-constructible).
type ExitError struct{ Code int }
```

Every member the port does **not** carry was cut against the verified consumer union:

- **No `Dir` on `ProcessSpec`** — no consumer sets `cmd.Dir` (`setupCommand` at `process_executor.go:135`, `newPipelineCmd` at `pipeline.go:59`); every consumer runs in the process cwd.
- **No `LookPath` on the runner** — the pwsh probe (`shell.go:136`, `windowsShellWrapper`) is shell-translation logic that stays put; `processExecutor.LookPath` (`process_executor.go:496-498`) remains a direct method implementing the internal `commandExecutor` interface (`reader.go:20-23`) and `tools.CommandExecutor`. It is a gate predicate-scope exclusion (Decision 5), not a port member.
- **No `Close()` on `ProcessHandle`** — reader-level ownership plus `Wait`-based reaping reproduces today's cleanup 1:1 (Decision 4, contract ③). A `Close` would duplicate the reaping contract and give fakes a second lifecycle to fake.
- **No `StartPipeline`** — exactly one pipeline consumer (`RunPipeline`); the wire/start interleave is pinned behavior-preserving (Decision 4, contract ①); the alternative was evaluated and rejected (doubles the fake surface to buy an unobservable ordering difference).

The handle getters carry **no error returns**: the adapter creates pipes inside `Start` *before* `cmd.Start`, preserving the `os/exec` ordering that keeps pipe-creation failures structurally unreachable (the cataloged `StdoutPipe`/`StderrPipe` structurally-unreachable entries, `process_executor.go:152-154` and `:156-158`). A getter after a successful `Start` cannot fail, so the failure channel has no consumer.

### 3. Exit-signal contract: domain-typed divergence from ADR-060's raw-error convention

The adapter converts **any** `*exec.ExitError` returned by `Wait()` into `tools.ExitError{Code: exitErr.ExitCode()}` — not only when `Exited()`. A signal-killed child yields an `*exec.ExitError` with `ExitCode() == -1`; today the type assertion at `RunCommand` succeeds for it, producing a result with code −1 and no error (`process_executor.go:105-106`). Converting only exited processes would flip that to a returned error — a behavior change to tool outputs, forbidden by the non-goals. Code −1 = signal-killed is today's behavior, made explicit and fake-constructible.

Non-`ExitError` wait failures pass through `Wait()` **unconverted** and keep today's handling: the error is returned, `RunCommand` reports exit code 1, and `formatPipelineResult` propagates the non-`*tools.ExitError` case as before. Consumers discriminate via `errors.As` at the three extraction sites (`process_executor.go:105-106`, `process_executor.go:284`, `pipeline.go:177-178`), which switch from the raw type assertion to `errors.As(err, *tools.ExitError)` in the same change.

**Declared divergence from ADR-060 §6's raw-error convention.** ADR-060 ruled that ports pass raw stdlib errors and consumers discriminate with `errors.As`/`errors.Is`; two raw-convention carriers exist today: `analysis/health.go`'s `errors.As(err, &exitErr)` guard discriminating `*exec.ExitError` through the `ToolchainRunner` port (`health.go:234-235`), and `CommandExecutor`'s doc-comment promise that execution failures surface as `*exec.ExitError` with stderr included (`executor.go:15`). Both stay **raw-stdlib — documented, not converted** (ADR-060 §6); `ProcessRunner` is domain-typed from day one. `ToolchainRunner` alignment to the domain-typed convention is a separate follow-up ADR (it touches `runLint`, the dev/release coverage paths, and the 22-site real-adapter test surface named in ADR-060 §9).

**Why domain-typed is necessary, not stylistic:** `os.ProcessState` cannot be fabricated with a chosen exit code outside the `os` package — a zero-value `&exec.ExitError{}` yields `ExitCode() == -1`. Without the domain type, the `exit 3 → Code == 3` branches are fake-untestable and this refactor's catalog-reduction claims are unmeetable.

Signal identity is deliberately dropped at the port (the Code −1 convention): grep-verified zero consumers of `Sys().(syscall.WaitStatus)` in workspace production files — the only signal-identity observers are the real-process tests, which stay.

### 4. Pipeline lifecycle contracts (four pinned)

Four contracts are pinned here as the behavior-preservation reference for the adapter and its tests. Any deviation from these is a real behavior change, not a refactor detail.

1. **Validity window + wire/start interleave.** `Stdout()`/`Stderr()` are valid only after the owning handle's `Start` returns; the adapter creates pipes inside `Start` before `cmd.Start`. The pipeline wire/start interleave is pinned behavior-preserving: `Start(spec[0])` → `spec[1].Stdin = handle[0].Stdout()` → `Start(spec[1])` → … — start order remains 0..n−1, the read-end fd exists from `Start(spec[0])`'s return, and capture begins only after all starts in both designs (pipe-buffer exposure identical).
2. **fd fast-path asymmetry.** Today's zero-copy stdin inheritance rests on wiring an `*os.File` read-end into `cmd.Stdin` (os/exec's `*os.File` fast path — direct fd inheritance, no copy goroutine). The adapter created the pipes, so it type-asserts its own handed-out read-ends back out of `spec.Stdin` and preserves direct inheritance; genuinely foreign `io.Reader`s get documented copy semantics (os.Pipe + copy goroutine under the adapter's `WaitDelay`). The asymmetry is stated here, not left as folklore.
3. **Close ownership.** Adapter-owned closes are exactly `cmd.Wait()`'s documented pipe closes, nothing more. The caller owns the pre-`Wait` lifetime: early-exit cleanup closes handed-out readers and reaps started handles (`Wait()` each). The happy-path post-`Wait` double-close is tolerated and its error ignored — today's behavior. Handed-out readers are `io.ReadCloser`; the executor's `closePipes` logic maps onto the port without new surface.
4. **Env semantics (adapter-side).** `nil` `spec.Env` → `cmd.Env` stays nil (pure inherit — the pipeline path's constant state; `PipeCommands`' params struct carries no env field, `shell.go:335-340`). An overlay → `os.Environ()` + append with exec.Cmd's documented dedup-keeps-last semantics; random map iteration is immaterial (dedup is per-key). The `os.Environ()` + overlay assembly (today at `process_executor.go:143-148`) **moves into the adapter**, which receives an unexported environ hook for its own tests — mirroring the `osGetwd` var precedent (`process_executor.go:25-29`). With the assembly inside the adapter, Decision 5's zero-allowance gate claim is cleanly true rather than predicate-tolerated.

### 5. Gate: `verify-tools-process-import` (lifecycle-token predicate, zero allowances)

A new Makefile gate, scoped to `internal/tools/workspace` **production files** (non-`_test.go`), wired into `check`/`check-full`/`test` (mirroring the `verify-tools-toolchain-import` wiring). Predicate: lifecycle tokens — `exec.Command` (spelled to also match `exec.CommandContext`), `exec.Cmd`, `syscall.SysProcAttr`, and `configureProcAttrs` — with **zero allowances**.

Two sites inside the gate's file scope are documented here as **scope decisions, not gate allowances**: `processExecutor.LookPath`'s `exec.LookPath` delegation (`process_executor.go:496-498` — a delegation-wrapper per the #1431 batch) and the pwsh probe (`shell.go:136` — platform-specific, Windows-only). Neither matches a predicate token (`exec.LookPath` is not in the token set), so neither needs an exclusion list entry; they are recorded so the scope is explicit and a future predicate widening re-adjudicates them by name.

**Why lifecycle tokens, not an import ban:** workspace production legitimately keeps the `os/exec` import after this refactor — the `LookPath` delegation (`process_executor.go:497`) and the pwsh probe (`shell.go:136`) both call `exec.LookPath` — so a package-level import ban would trip sanctioned code. The generalization argument holds tools-wide: `analysis/health.go`'s `*exec.ExitError` discrimination (`health.go:234-235`) is a legitimate `os/exec` *type reference* an import-ban predicate would fail. The token set matches only process-spawning lifecycle, never type references.

**Tools-wide raw-exec inventory (post-refactor state):**

| Site | Disposition |
| --- | --- |
| `internal/tools/workspace` cluster (the ~40 fault-injection sites) | **Eliminated by this refactor** — the lifecycle moves behind the port; the sites become fake-drivable |
| `internal/tools/developer/dev.go:58` (`realExecutor.Execute` → `exec.CommandContext(...).CombinedOutput()`) | **Documented residue, NOT migrated** — named follow-up issue. Four grill-verified grounds: (1) zero developer fault-injection catalog entries exist to justify the seam; (2) the error path is already deterministically drivable by tests; (3) migration is unfunded signature churn through `newDevManager`/`developer/registration.go`; (4) the workspace-scoped gate has no exception to sanction it — the ADR-060 §11 residual pattern, recorded rather than force-fit |
| `internal/tools/workspace/shell.go:136` (`exec.LookPath("pwsh")`) | Predicate-scope exclusion (platform-specific, Windows-only) |
| `internal/tools/workspace/process_executor.go:496-498` (`exec.LookPath` delegation) | Predicate-scope exclusion (delegation-wrapper) |

### 6. Test-layer asymmetry: no default-runner fallback

- **Production constructor census:** `newprocessExecutorWithFS(fs, runner)` becomes the sole production constructor; the zero-arg `newprocessExecutor()` (`process_executor.go:63-65`) is deleted. Its **69 test-only call sites across 10 test files** migrate: `process_executor_stream_test.go` ×27, `process_executor_unit_test.go` ×19, `reader_test.go` ×7, `pipeline_test.go` ×5, `process_executor_security_test.go` ×4, `process_executor_test.go` ×2, `process_executor_stress_test.go` ×2, `process_executor_posix_test.go` ×1, `process_executor_stream_unix_test.go` ×1, `registration_test.go` ×1.
- **Singular in-package helper:** one `workspace` in-package `_test.go` helper replaces the zero-arg constructor and defaults to the **real adapter** — the workspace analog of ADR-060 §9's 22 surviving `toolchain.NewGoRunner` test sites: real-adapter-over-defaults remains the verification surface for tests that do not care about the runner. **Anti-extension ruling:** the `verify-tools-process-import` gate must never be extended to `_test.go` files; doing so would break the helper and the real-adapter test surface exactly as ADR-060 §9 ruled for the toolchain gate.
- **`workspace/default_fs.go` retired:** its only production referencer is the dying zero-arg constructor (`process_executor.go:64` — grep-verified sole use of `defaultFS`). With full injection and no fallback, the ADR-055 amendment (below) shrinks the sanctioned-list.
- **Fake-based tests construct explicitly:** tests that exercise runner behavior call `newprocessExecutorWithFS(fs, fakeRunner)` directly — choosing the fake is the assertion.
- **Nil-guard message updated:** the current panic message points at the deleted constructor (`process_executor.go:74`: "use newprocessExecutor() for the default OS filesystem") and is updated in the same change to point at the injected seam.

## Consequences

- **Positive**: single process-runner construction at the composition root (`internal/infrastructure/di/process_factory.go`); the ~40-site workspace cluster becomes deterministically fake-testable; `fault-injection-required` stops growing mechanically from workspace branches; the OS-free property of `internal/tools/workspace` for process-lifecycle code is mechanically gated.
- **Net catalog reduction ≥24**: 19 fake-based sites (their `fault-injection-required` pins are removed once driven by `tools.ExitError`-carrying fakes) plus 5 adapter-internal sites (`proc_posix.go` — the pins describe the port's implementation, so their correct treatment is adapter-internal tests with an injected kill function and a clock hook per ADR-036, not catalog entries).
- **Regression-visible**: none — behavior-preserving by the four pinned contracts of Decision 4; the exit-signal convention (Decision 3) reproduces today's observable codes exactly, including −1 for signal-killed children.
- **Accepted divergence**: `ProcessRunner` is domain-typed while `CommandExecutor`/`ToolchainRunner` remain raw-stdlib (Decision 3) — a documented two-convention state until the ToolchainRunner follow-up ADR lands.
- **Known-gap**: `internal/tools/developer/dev.go:58` raw-exec residue with a named follow-up issue (Decision 5 inventory).
- **Modelith decision**: **glossary entries** (vocabulary, not entities) for `ProcessRunner`, `ProcessSpec`, `ProcessHandle`, and `ExitError` in the tell-me-go domain model — mirroring the `CoverageSummary` resolution in ADR-060 §6 that silenced the modelith-drift advisory.
- **ADR-055 amendment**: the workspace `defaultFS` fallback is retired together with the zero-arg `newprocessExecutor()`; `verify-tools-adapter-import`'s sanctioned list shrinks to `internal/tools/analysis/default_fs.go` only — the defaultFS rationale (dozens of consumers) does not transfer to a one-consumer runner chain. Amended in `docs/adr/2026-08-tools-filesystem-injection.md` under Related.
