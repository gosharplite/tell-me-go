# ADR-0010: Phase 10 — Production Complexity Reduction in Integrations Layer

**Status:** Proposed  
**Date:** 2026-05-07  
**Author:** AI Architect  
**Supersedes:** None (new phase)

---

## Context

All 9 roadmap phases are complete. Cyclomatic complexity analysis reveals **13 production functions at complexity 9** — one step below the gocyclo alert threshold of 10. These functions cluster heavily in the integrations/services layer and share three distinct structural patterns. Additionally, **10+ test functions exceed complexity 10** (max: 17 in `TestHistoryNavigationFlags`).

The codebase is architecturally clean (no circular dependencies, no layer violations, no God Objects), but these functions are fragile string-builders, sequential pipelines, and switch-chain constructors that grow unbounded as provider APIs evolve.

## Decision

Phase 10 will systematically reduce all 13 production functions to complexity ≤5 using three archetype-specific decomposition strategies, then enforce a CI gate at complexity ≤10.

### Strategy by Archetype

| Archetype | Count | Strategy | Pilot |
|-----------|-------|----------|-------|
| **A: Builder functions** | 6 | Extract field-level formatters; keep iterator thin | `formatBranchPolicies` |
| **B: Pipeline functions** | 4 | Extract each pipeline stage into a method | `SendChat` |
| **C: Switch-chain constructors** | 2 | Strategy pattern with provider registry map | `NewClient` |
| **D: Error-path utility** | 1 | State-machine decomposition | `AtomicWrite` |

### Execution Order

1. Write this ADR
2. Pilot refactor: `formatBranchPolicies` (Archetype A)
3. Quick win: `NewClient` (Archetype C) — eliminates code duplication in `default` case
4. Roll out Archetype A pattern: `formatPrThreads`, `getPipelineLogContent`, `confluenceSearch`, `markdownToXhtml`, `resolveSpaceID`
5. Roll out Archetype B pattern: `SendChat`, `JiraSearchIssues`, `GetSuggestions`, `RollbackTurns`
6. Refactor `RotateSession` (Archetype C) and `AtomicWrite` (Archetype D)
7. Add `gocyclo -over 10` to CI lint configuration
8. (Follow-up phase) Simplify test functions exceeding complexity 10

### Design Principles

- **Do not change behavior.** Every refactor must pass existing tests unchanged.
- **Extract, don't rewrite.** New methods are carved from existing code, not written from scratch.
- **One function at a time.** Each refactor is a standalone commit with its own test verification.
- **Complexity target:** All production functions ≤5 after decomposition.

## Consequences

### Positive
- Format/render functions become resilient to API surface changes
- Provider constructors eliminate duplicated code
- CI gate prevents regression
- Each extracted method is independently testable

### Negative
- More total functions (larger surface area for godoc)
- Marginal indirection cost (method calls vs inline code)
- Requires discipline to maintain the CI gate

### Neutral
- No API surface changes; existing callers unaffected
- No database migrations or config changes
