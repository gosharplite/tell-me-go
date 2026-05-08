<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-030: Release Branch Synchronization Policy

## Status
Accepted

## Decision Date
2026-05-05

## Context

The `tell-me-go` project operates two long-lived branches:

| Branch | Role |
|---|---|
| `dev` | Integration branch — all feature work and routine commits land here first |
| `main` | Release branch — receives a single `--no-ff` merge from `dev` per release, which is then tagged |

The release flow, codified in `docs/sop/lifecycle/public_release.md`, was historically:

1. Merge `dev → main` with `--no-ff`.
2. Tag the merge commit on `main`.
3. Push everything: `git push origin main dev --tags`.
4. Check out `dev`.

### The Defect

Step 3 is a **silent no-op for `dev`**. The only commit affecting `dev` would have been the back-merge from `main` — but no back-merge step existed. The `git push origin main dev --tags` succeeded as a whole even though the `dev` portion had nothing to push. The operator saw success and moved on.

Over **109 release cycles**, this produced 109 release-merge commits on `main` that never reached `dev`. At the time of discovery, `origin/main` was 109 commits ahead of `origin/dev` despite every release report indicating success.

### Why the Bug Stayed Invisible

| Mask | Why it hid the divergence |
|---|---|
| **Combined push** | `git push origin main dev --tags` reported success because `main` and the tags really did push; the no-op on `dev` was indistinguishable from success in the operator's eyes. |
| **No post-release verification** | The SOP had no step comparing `origin/main` and `origin/dev` after the release. The "release succeeded" signal was the absence of an error, not a positive convergence check. |
| **Forward merges still applied cleanly** | Each subsequent `dev → main` merge succeeded because `dev`'s commit graph was a strict subset of `main`'s — `git merge` produced clean fast-forward-style merges with no conflicts. The repository's apparent health (no merge conflicts) confirmed the wrong hypothesis. |
| **`git status` on `dev` looked clean** | Locally, `dev` was always "up to date with origin/dev" — which was true, but the relevant comparison was `dev` vs. `main`, which the SOP never ran. |

### Why `git reset --hard origin/main` Aggravated the Symptom

Step 4.1 of the old SOP began with `git reset --hard origin/main` to "sync" the local `main` before the release merge. This silently discarded any local divergence on `main`. While not the root cause of the back-merge bug, it modeled a destructive-sync mindset that made the surrounding workflow tolerant of silent state loss — exactly the wrong conditioning for an operator about to perform a release.

### Why an Architectural Decision Is Warranted

This is not a one-off bug fix. The defect is a **policy gap**: there was no documented contract that `origin/main == origin/dev` after a release. Without that contract, no individual SOP step could be unambiguously identified as missing. Future maintainers — including the LLM agents that execute this SOP — will independently re-derive workflows; without a recorded policy, the same gap can re-emerge in a different form (e.g., during a CI/CD migration, a release-script rewrite, or a switch to a different branching model).

This ADR establishes the contract so that any future change to the release flow can be evaluated against an explicit invariant.

## Decision

Adopt a **strict convergence policy**: after every successful release, `origin/main` and `origin/dev` MUST point to the same commit. Convergence is treated as a first-class invariant of the release process — not as a happy-path side-effect.

The policy is enforced by three independent procedural gates in `docs/sop/lifecycle/public_release.md`. The redundancy is deliberate: any single missed step is caught by the next gate, providing defense in depth.

### 1. Pre-Flight Gate (SOP Step 1.3)

Before any release work begins, the SOP computes:

```bash
AHEAD=$(git rev-list --count origin/main..origin/dev)
BEHIND=$(git rev-list --count origin/dev..origin/main)
```

The release proceeds only if `AHEAD > 0` AND `BEHIND == 0`. Two failure modes are caught here:

- **Empty release** (`AHEAD == 0`): `dev` has no new commits. Aborting prevents burning a version number on an empty merge commit.
- **Back-merge gap** (`BEHIND > 0`): `main` contains commits absent from `dev` — the historical bug's signature. Aborting prevents the new release from being layered on top of an unresolved divergence. The SOP includes a documented recovery procedure: merge `origin/main` into `dev`, push, restart the SOP from Step 1.

### 2. Mandatory Back-Merge (SOP Step 4.6)

After tagging and pushing `main`, the SOP explicitly merges `main` back into `dev`:

```bash
git checkout dev
git merge main --ff-only
```

A fallback path handles the realistic case where new commits were pushed to `dev` during the release window (`--ff-only` will fail in that case): rebase local `dev` onto `origin/dev`, then merge `main` with `--no-ff` and a `chore: sync release vX.Y.Z back to dev` message. This preserves the back-merge as a first-class commit on `dev` regardless of concurrent activity.

### 3. Post-Flight Gate (SOP Step 4.8)

After pushing `dev`, the SOP re-runs the same `rev-list --count` checks as Step 1.3, this time requiring **both** counts to be `0`. Any non-zero result means the release is incomplete and the operator must investigate before declaring success.

The pre-flight and post-flight gates use the **identical** `rev-list --count` pattern so that operators only learn one mental model of "convergence."

### 4. Supporting Decisions

These accompany the three gates and were adopted as part of the same SOP rewrite:

#### 4.1 Tag Only Verified Commits (SOP Step 4.2 → 4.4)

The build (`go build -ldflags ...`) and the race-enabled smoke test (`go test -race -count=1 ./...`) are now executed on the `dev → main` merge commit **before** tagging. Tags are immutable contracts in this project (see existing SOP Best Practice "Never Re-tag"); tagging unverified code burns version numbers when the build fails. The smoke test specifically catches semantic conflicts that a clean text merge can hide — e.g., interface evolution on `dev` that compiles in isolation but breaks consumers that landed on `main` independently.

#### 4.2 No Destructive Sync

Replace every `git reset --hard origin/<branch>` in the SOP with `git pull --ff-only origin <branch>`. Hard resets silently discard local divergence; `--ff-only` surfaces it as an error and forces operator investigation. The single remaining `git reset --hard origin/main` in the SOP appears only inside the failure-recovery prose of the smoke-test step (Step 4.3), where it discards a **local-only** failed merge commit — a legitimate use of the destructive operation that does not violate the no-destructive-sync principle.

#### 4.3 Split Push (SOP Step 4.5 → 4.7)

The combined `git push origin main dev --tags` is replaced with two separate pushes: `main --tags` first, then `dev` after the back-merge completes. The combined push was a primary mask of the historical bug because it allowed a no-op on `dev` to ride on the success of the `main` push. Splitting the push makes each branch's state transition independently observable.

## Consequences

### Positive

- **Branch divergence is structurally impossible** if the SOP is followed. The post-flight gate cannot be passed without `origin/main == origin/dev`.
- **Defense in depth.** A single missed step is caught by the next gate. Skipping the back-merge (Step 4.6) is caught by the post-flight gate (Step 4.8). Skipping the post-flight gate is caught by the next release's pre-flight gate (Step 1.3 of release N+1). The system is self-healing across at most one release cycle.
- **Failed builds no longer waste version numbers.** Tagging happens after build + smoke test, so a broken merge is discarded before any tag is consumed.
- **Concurrent `dev` activity during a release is handled gracefully.** The Step 4.6 fallback (rebase-then-no-ff-merge) accommodates feature commits landing on `dev` while a release is in flight, without blocking the release or losing the back-merge.
- **The policy is recoverable.** If a future regression re-introduces divergence, the Step 1.3 abort gate will surface it on the very next release attempt, with a documented one-shot recovery procedure.
- **The mental model is single.** Operators (human and LLM) learn one pattern — `git rev-list --count A..B` returns 0 — and apply it identically pre-flight and post-flight.

### Negative

- **Each release now requires two pushes instead of one.** Marginal cost; not a real concern given release cadence.
- **The pre-flight gate will permanently block any release attempted from a divergent state**, requiring an explicit recovery merge before the SOP can resume. This is intentional but may surprise operators familiar with the old workflow. The recovery procedure is documented inline in Step 1.3 to reduce friction.
- **The smoke test (`go test -race -count=1 ./...`) on the merge commit adds ~60–90 seconds to each release.** Acceptable at the current scale (~65 packages); will need re-evaluation if the suite grows materially.
- **Retroactive convergence of the existing 109-commit divergence is a separate operational task** (out of scope for this ADR and the SOP rewrite). Until performed, the very first release after this ADR will be blocked by the Step 1.3 pre-flight gate — by design — until the historical divergence is resolved with a one-time recovery merge.

### Neutral

- **No change to the branch model.** `dev` and `main` remain long-lived; no rebasing of `dev` onto `main`; no switch to trunk-based development.
- **No change to the public artifact contract.** Release tags, release commits on `main`, and the `--version` output of the binary are byte-identical to what the old SOP produced when it didn't fail.
- **No new tooling required.** The policy is enforced entirely with `git rev-list --count`, which is available in every supported git version.

### Alternatives Considered

| Alternative | Rejected Because |
|---|---|
| **Rebase `dev` onto `main` after each release** instead of merging back | Rewrites `dev`'s history. Breaks any in-flight feature branches based on `dev`. Forces every contributor to `git pull --rebase` after every release or face conflicts. |
| **Single trunk (delete `dev`)** | Too large a workflow change for the current contributor base and CI maturity. Worth revisiting if/when CI gates can substitute for `dev`'s integration role, but that is a separate ADR. |
| **Automated release script that performs back-merge automatically** | Rejected. The project strictly relies on explicit SOP execution (`docs/sop/lifecycle/public_release.md`) by operators/LLM agents. Automating the workflow hides the invariant and masks the explicit policy contract. |
| **Makefile check that fails the build if `origin/main..origin/dev` is non-zero** | Rejected. The SOP's pre-flight and post-flight gates strictly enforce convergence at the moment of release. An out-of-band Makefile check is unnecessary. |

## Implementation Plan

This ADR documents a workflow change rather than a code change. The implementation is the SOP rewrite itself, executed in five sequential commits prior to this ADR:

| Task | Description |
|---|---|
| T1 | Replace destructive `git reset --hard origin/main` with `git pull --ff-only origin main` in SOP Step 4.1; add "No Destructive Resets" Best Practice |
| T2 | Add mandatory back-merge (SOP Step 4.6), split push (4.5/4.7), and post-flight convergence check (4.8); add "Always Back-Merge" Best Practice; expand Implementation Checklist from 7 to 10 items |
| T3 | Reorder SOP Step 4 so build + smoke test precede tagging; add `go test -race -count=1 ./...` as Step 4.3 |
| T4 | Add pre-flight divergence check at SOP Step 1.3; harden Step 1.2 sync to `git pull --ff-only origin dev` |
| T5 | This ADR; SOP Objective line updated to cite ADR-030 |

Acceptance criteria for the SOP rewrite:

| Check | Target |
|---|---|
| `git reset --hard origin/main` removed from sync paths | ✅ Only remaining occurrence is in Step 4.3 failure-recovery prose |
| SOP Step 1 has a divergence abort gate | ✅ Step 1.3 |
| SOP Step 4 has a back-merge step | ✅ Step 4.6 |
| SOP Step 4 has a post-flight convergence check | ✅ Step 4.8 |
| Build + smoke test execute before tagging | ✅ Step 4.2–4.3 precede Step 4.4 |
| Implementation Checklist reflects new substeps | ✅ 10 items, including back-merge and convergence check |

## Future Work

- **`ConfigApplyFailed`-style observability.** If the back-merge fallback (Step 4.6's `--no-ff` resolution path) is exercised frequently in practice, consider emitting a structured log line for telemetry. Not pursued now because the fallback is expected to be rare.

## References

- `docs/sop/lifecycle/public_release.md` — the SOP that implements this policy. All section numbers cited in this ADR (1.3, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8) refer to this document.
- ADR-029 (`2026-05-fallible-reconfigure-delegate-chain.md`) — most recent prior ADR; cited here so future readers can navigate the ADR sequence.
- `docs/sop/standards/adr_standards.md` — ADR authoring process; this ADR follows its required format.
- `docs/adr/README.md` — ADR index (this ADR is registered as ADR-030).
