# ADR-062: Relocate internal/infrastructure/encoding to internal/pkg/encoding; General Tools→Infrastructure Import Gate and layerShared Rule

**Status:** Accepted
**Date:** 2026-08
**Related:** [Issue #1336](https://github.com/gosharplite/tell-me-go/issues/1336), [ADR-055](2026-08-tools-filesystem-injection.md), [ADR-060](2026-08-toolchain-runner-injection.md), [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md), [ADR-063](2026-08-sop-documentation-governance.md)

## Context

The tools (application) layer imports the locale-decoding utility `internal/infrastructure/encoding` directly in production code — the third tools→infrastructure edge, and the only one that is ungoverned: ADR-055 confined `persistence` (via `verify-tools-adapter-import`), ADR-060 confined `toolchain` (via `verify-tools-toolchain-import`), but `encoding` had no ADR adjudicating the direction and no gate confining it.

Verified state: three production importers — `internal/tools/workspace/pipeline.go:18` (call sites `:151`, `:155`), `internal/tools/workspace/process_executor.go:22` (call sites `:174-175`), and the infra-internal `internal/infrastructure/exec/executor.go:10` (`DecodeBytes` at `:18`, `:23`). The package exports exactly `WrapReader(io.Reader) io.Reader` and `DecodeBytes([]byte) []byte`, has zero module-internal dependencies (the Windows variant imports `golang.org/x/text`, already in `go.mod` at v0.39.0), and its test file is white-box (`package encoding`, testing unexported `isUTF8Env`/`decodeFromReader`) — the move preserves it.

The edge was invisible to both architectural instruments: the layer gate's `layerTools` rule forbids only `{application, cmd}` (`architecture.go:318-319`), no `layerShared` rule existed in the `:301` rules slice, and ADR-056's `familyOf()` returns `""` outside `internal/domain/`, so no transitive-whitelist row can express a tools→infrastructure edge (ADR-060's own Context). The only other infrastructure imports in `internal/tools/` production are the two sanctioned `default_fs.go` files (ADR-055).

The R1 lineage — persistence → ADR-055 (injection + gate), toolchain → ADR-060 (injection + gate), encoding → nothing — shows the established pattern is remedy paired with a mechanical gate: a documentation note cannot fail a build and therefore cannot prevent recurrence. This ADR ships both the instance removal (relocation) and the class prevention (generalized gate + directional rule).

## Decision

### 1. Relocation

`internal/infrastructure/encoding/` (four files) is relocated to `internal/pkg/encoding/` via `git mv`, no content change: `encoding.go`, `encoding_other.go` (`//go:build !windows`), `encoding_windows.go` (`//go:build windows`), `encoding_test.go` (white-box `package encoding`). The three importers are updated. Zero behavioral change; `encoding` becomes the ninth `internal/pkg` directory.

### 2. Directional utility-home criterion

`internal/pkg` is the home of dependency-free shared utilities. **`internal/pkg` may depend only on `internal/domain` and other `internal/pkg`.** Anything requiring an adapter belongs in `internal/infrastructure`, with the port defined in `internal/domain` (the ADR-055/060 injection pattern). The criterion is anchored in the SOP §3 "Internal/pkg Directional Boundary" bullet.

### 3. General gate

New Makefile target `verify-tools-infrastructure-import` — the generalized tools→infrastructure import gate. Predicate (module-prefixed, avoiding the bare composition-root string literals at `architecture.go:296-297` / `exit_query.go:235-236`): `grep -rn 'github.com/gosharplite/tell-me-go/internal/infrastructure/' internal/tools/ --include='*.go'`, excluding `_test.go:` lines, the two sanctioned `default_fs.go` paths, and `analysistest/`/`toolstest/`. Dual POSIX/PowerShell form mirroring `verify-tools-toolchain-import`. Position: 8th `test:` prerequisite, after `verify-tools-adapter-import` (6th) and `verify-tools-toolchain-import` (7th) — the ADR-055/060 gates remain primary and earlier. Wired into `.PHONY`, `help`, `test`, `check`, `check-full`. The triage message is self-locating: relocate-if-utility → `internal/pkg/` (Decision 2); inject-if-adapter per ADR-055/060; otherwise a new ADR for the edge (Decision 5).

### 4. layerShared rule

Fifth rule in `checkLayerViolations`' rules slice (`architecture.go:301`): `{SourceLayer: layerShared, Forbidden: []string{layerInfrastructure, layerTools, layerApplication}}`, with the inline comment `// ADR-062: internal/pkg (Shared) must not depend on Infrastructure, Tools, or Application layers.` Reason string (final): **"Shared (internal/pkg) must not depend on Infrastructure, Tools, or Application layers."** `cmd` stays out of Forbidden — `checkGeneralCmdImport` (`architecture.go:380`) already covers pkg→cmd exactly once. Lands green: `testfixtures → domain/ports` is the only module-internal pkg import; `encoding` post-move has none. Boundary test: `TestArchitectureManager_CheckLayerViolations_LayerShared` (seven rows, fresh map per row, asserting count + category + target + reason).

### 5. Gate-Exception Contract

An edge that is neither a dependency-free utility (triage 1) nor an injectable adapter (triage 2) requires an exception: **a NEW ADR adjudicating the edge** (the R1 pattern — persistence → ADR-055, toolchain → ADR-060), architect-sanctioned, ADR-cited, plus the whitelist edit (the gate target's exclusion list) — all in the same change. An exception is NOT an amendment to this record.

### 6. R1 lineage

persistence → ADR-055 (injection + `verify-tools-adapter-import`); toolchain → ADR-060 (injection + `verify-tools-toolchain-import`); encoding → this ADR (relocation + generalized `verify-tools-infrastructure-import` + `layerShared` rule). The class, not the instance, is the defect: the generalized gate makes the whole tools→infrastructure class unable to recur.

## Consequences

Positive: the third tools→infrastructure edge is removed by construction; the general gate and the `layerShared` rule prevent the class from recurring; `internal/pkg`'s boundary is explicit and gate-enforced; the ADR-055/060 gates remain primary and earlier.

Negative: a fifth rule in `checkLayerViolations` adds a small constant cost to architecture verification; the gate's PowerShell branch is inspection-verified only (no pwsh on dev boxes, no CI — repo norm); the one-time SOP corrections are unguarded prose per the maintainer veto (recorded in ADR-063).

Known gap (cross-referenced): the `layerUnknown` classification-waiver class (`internal/race` status quo) is a documented known-gap with no owner and no priority (recorded in ADR-063).
