# ADR-063: SOP Documentation-Governance — Maintainer Note-Only Veto and One-Time Prose Correction

**Status:** Accepted
**Date:** 2026-08
**Related:** [Issue #1336](https://github.com/gosharplite/tell-me-go/issues/1336), [ADR-062](2026-08-encoding-relocation-and-tools-infrastructure-gate.md)

## Context

Issue #1335's architecture grill round designed a full SOP documentation-governance apparatus — a generated directory map, an inventory gate, and fail-on-unknown semantics for new top-level directories. The maintainer veto (2026-08-12) narrowed the scope of issue #1336: **no documentation-governance apparatus** — no generated map, no inventory gate, no fail-on-unknown. The SOP corrections ship once as prose, unguarded, with subsequent drift accepted per the repo's prose norm. The edge-governance class (ADR-062: the generalized `verify-tools-infrastructure-import` gate + the `layerShared` rule) ships unchanged.

## Decision

### 1. Maintainer note-only veto

The SOP documentation-governance apparatus proposed by issue #1335 is **rejected by maintainer decision**. No `cmd/sopmap`, no `verify-sop-map`, no generated map, no fail-on-unknown. This record documents the veto; the SOP's drift after the one-time correction is accepted per the repo's prose norm (a documentation note cannot fail a build, and no gate is added for it).

### 2. One-time SOP prose correction

`docs/sop/technical/architecture_and_packages.md` is corrected once, as prose, unguarded: the §1 `internal/pkg` listing is corrected to the family-level set (3→9 — `encoding` is the ninth `internal/pkg` directory); the three map/prose phantoms are fixed (`orchestration/` and `llmcoord/` in the ASCII map, `orchestration` in the §1 prose); and a §3 directional bullet is added after the ADR-055 bullet citing ADR-062 Decision 2. Drift after this correction is accepted; no gate enforces it.

### 3. layerUnknown known-gap (no owner, no priority)

The `layerUnknown` classification-waiver class (`internal/race` status quo) is a **documented known-gap with no owner and no priority**. The #1335 round's escalation ("immediate, owned, ahead of any other queue item") is overridden by maintainer decision. The class is recorded, not escalated.

### 4. Follow-ups (recorded, not escalated)

- **Quality-model sync** — `GateKind.verify` names 6 of the Makefile's 14 verify gates; the ADR-entity definition names 4 of 61 ADRs. A full sync (all 14 gates, current ADR examples) is an architect-owned model-hygiene task, not a partial one-line patch.
- **PowerShell execution verification** — the PowerShell twins of the text-scanning gates (including `verify-tools-infrastructure-import`, ADR-062) are inspection-verified only (no pwsh on dev boxes, no CI). If pwsh becomes available on a dev box, execute and verify them.

## Consequences

Positive: the maintainer veto is recorded and unambiguous; the SOP's unguarded prose status is explicit; the edge-governance remedy (ADR-062) is unaffected.

Negative: the SOP §1 listing and directory map will drift from the tree with no gate detecting it (accepted — repo prose norm); the `layerUnknown` class remains a recorded gap with no owner and no priority.

The §5 README backlog correction (links for ADR-054, 055, 056, 060, 061 added to the main README Design Decisions section) ships with this record.
