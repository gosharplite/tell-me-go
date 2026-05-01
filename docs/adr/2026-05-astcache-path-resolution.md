# ADR-022: astCache path resolution via injected baseDir

**Status:** Accepted
**Date:** 2026-05
**Deciders:** Architect, Coder
**Supersedes:** N/A
**Superseded by:** N/A
**Related:** Issue #108

## Context

`astCache` was implicitly coupled to the process-global current working
directory (`os.Getwd()`). Every call to `os.Stat(path)`, `parser.ParseFile`,
and every cache key (map, FIFO order, singleflight) used caller-supplied
paths directly. When callers passed relative paths — which all 5 production
call sites did — correctness depended on the unsynchronized, mutable
`os.Getwd()`.

This forced tests to use `os.Chdir()`, which is process-global and
non-parallelizable. It also blocked future concurrent multi-repository
analysis from a single process.

## Decision

**`astCache` receives a `baseDir string` at construction time. All path
resolution happens inside the cache via a single `absPath` helper. Callers
continue to pass paths as-is and remain agnostic of the root.**

### Resolution rules (implemented in `absPath`)

1. If `baseDir` is empty: path is used as-is (backward compat for
   zero-value construction).
2. If the caller-supplied path is already absolute (`filepath.IsAbs`):
   path passes through unchanged.
3. Otherwise: `filepath.Join(baseDir, path)` produces the absolute path
   used for all I/O and cache keys.

### Affected methods

| Method | Resolution point |
|---|---|
| `Get(path)` | Resolves at top; `abs` used for `getValidEntry`, `os.Stat`, `parser.ParseFile`, `singleflight.Do`, `updateCache` |
| `GetCachedLineCount(path, info)` | Resolves before cache lookup |
| `GetFileSkeletonGo(filePath)` | Resolves before calling `Get` |
| `getValidEntry(path)` | Internal; resolves on entry |
| `updateCache(path, ...)` | Internal; resolves on entry |

### Composition root

`registration.go` injects `"."` — matching the existing `newIndexer(".")`
pattern. Tests that need a different root construct `newASTCache(tmpDir)`.

### Caller contract

Callers SHOULD pass paths relative to the cache's `baseDir`. Callers MAY
pass absolute paths (e.g., from `SP.IsPathSafe`) — these are explicitly
supported via the pass-through rule in `absPath`.

## Consequences

### Positive

- **No `os.Chdir` in any test.** `t.Parallel()` is restored everywhere.
- **Single point of resolution.** Adding a new analyzer or cache method
  cannot accidentally reintroduce `os.Getwd()` coupling.
- **Multi-repository analysis is unblocked.** Two `astCache` instances
  with different `baseDir` values can operate independently.
- **Cache key stability.** All keys are absolute paths, eliminating
  collision risk between same-named files in different directories.

### Negative

- **One additional field on `astCache`.** Negligible.
- **Callers passing already-absolute paths rely on pass-through behavior**
  which is documented but less explicit than stripping the prefix.
  Mitigated by the `absPath` helper being the single choke-point.

## Alternatives Considered

1. **Per-consumer `BaseDir` (issue #107).** Every call site joins paths
   before calling the cache. Rejected: violates DRY; the cache itself
   remains globally-coupled; must replicate at every current and future
   call site.
2. **`fs.FS` abstraction.** Premature. `parser.ParseFile` supports
   `fs.FS`, but cache invalidation via `ModTime()` has inconsistent
   semantics across virtual filesystems. Defer until a concrete
   sandboxing requirement appears.

## Compliance

- `go test -race -count=10 ./internal/tools/analysis/...` must pass
  cleanly on every commit touching `astCache`.
- New callers of `astCache` methods must pass paths consistent with
  the contract documented in this ADR.
- Any future change to path-resolution semantics requires an ADR update.
