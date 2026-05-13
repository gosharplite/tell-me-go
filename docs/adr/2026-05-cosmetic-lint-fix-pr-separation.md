<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-034: Cosmetic Lint Fixes Must Be Submitted as Standalone PRs

- **Status:** Accepted
- **Date:** 2026-05-13
- **Issue:** [#361](https://github.com/gosharplite/tell-me-go/issues/361)

## Context

PR [#357](https://github.com/gosharplite/tell-me-go/pull/357) mixed cosmetic `errcheck` lint fixes (`defer f.Close()` → `defer func() { _ = f.Close() }()`) across 5 unrelated test files with a race-detector timeout fix. While these lint fixes were prerequisites for `make check-full` to pass, conflating them with substantive changes:

1. **Dilutes review focus** — reviewers must mentally separate mechanical changes from behavioral ones.
2. **Confuses risk profiles** — a race-condition fix and a lint suppression carry vastly different risk; bundling them obscures this.
3. **Complicates git blame** — mechanical changes pollute the history of files touched for substantive reasons.

## Decision

We adopt the following team norm for all PRs:

### Rule: Cosmetic Lint Fixes Go in Standalone PRs

Cosmetic lint fixes — defined as changes that suppress linter warnings without altering program behavior — must be submitted as **separate, standalone PRs**, not mixed with behavioral, architectural, or feature changes.

This includes, but is not limited to:
- `gofmt` / `goimports` formatting changes
- `errcheck` suppressions (e.g., `defer func() { _ = f.Close() }()`)
- `staticcheck` / `gosimple` compliance edits
- Whitespace-only changes
- Import reordering
- Comment grammar/spelling fixes (when not on a hunk already under review)

### Stacked PR Pattern for Hard Prerequisites

If a lint fix is a hard prerequisite for a main change (e.g., `make check-full` must pass before the main PR can be submitted), use a **stacked PR**:

```
lint-fix-branch  ──(base)──▶  dev
       ▲
       │
feature-branch ──(base)──▶  lint-fix-branch
```

1. Submit the lint-fix PR first, targeting `dev`.
2. Branch the feature PR off the lint-fix branch.
3. Merge the lint-fix PR first; the feature PR's diff will then show only the substantive changes.

### Exception: Tiny Corrections in Already-Reviewed Hunks (≤5 lines)

> **Exception:** Tiny corrections (≤5 lines) to hunks already under review may be applied in-place within the same PR. Examples: comment clarifications, defensive nil guards, whitespace fixes in touched files. Rationale: round-tripping a 3-line edit through a separate PR adds more review overhead than it saves.

This exception applies only when:
1. The correction is in a file already modified by the PR.
2. The correction is ≤5 lines total (not per file).
3. The correction does not alter program behavior.

## Consequences

### Positive
- **Cleaner review experience**: Reviewers can focus on behavioral logic without noise.
- **Clearer git history**: `git blame` and `git log` show meaningful changes, not lint choreography.
- **Lower risk**: Mechanical and behavioral changes are validated independently.
- **Faster CI**: Standalone lint PRs are trivial to verify (no behavioral tests needed).

### Negative
- **More PRs**: Increases the number of PRs in flight; mitigated by the small-PR philosophy this project already follows.
- **Stacked PR overhead**: Requires coordination when lint fixes are prerequisites; the stacked-PR pattern formalizes this coordination.
- **Delayed lint satisfaction**: A feature PR may introduce new lint warnings that must wait for a separate PR; acceptable trade-off for review hygiene.

## References

- PR [#357](https://github.com/gosharplite/tell-me-go/pull/357) — motivating incident
- Issue [#361](https://github.com/gosharplite/tell-me-go/issues/361) — discussion and carve-out
