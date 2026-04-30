# Extract `session/context/` sub-package from `agent/session` monolith

## Category
[TECHNICAL DEBT] — Package Cohesion / Separation of Concerns

## Context
`internal/agent/session` currently contains **55 files (29 production + 26 test)** mixing 5+ distinct concerns in a single package. This is the largest package in the codebase by file count and a known navigability problem. ADR-008 ("Domain Decomposition Strategy") established the principle of decomposing oversized packages along concern boundaries; this issue applies that principle to the largest remaining offender.

ADR-025 ("UIBridge Decomposition") has already been executed — `ui_bridge*.go` files are now properly internally separated. The next-largest concern cluster is **context preparation**, which spans 7 production files with clear cohesion and a shared dependency graph distinct from the rest of the package.

## Files in Scope

| File | LOC (approx) | Role |
|---|---|---|
| `context_manager.go` | TBD | Public façade; orchestrates context build pipeline |
| `context_pipeline.go` | TBD | Pipeline stage execution |
| `context_strategy.go` | TBD | Strategy pattern for different context build modes |
| `context_transformers.go` | ~500 | Pure functions transforming history → context |
| `pruner.go` | TBD | Token-budget pruning |
| `gatekeeper.go` | TBD | Pre-send validation |
| `token_counter.go` | TBD | Token estimation |
| `pipeline_factory.go` | TBD | Pipeline construction |

Plus their `*_test.go` siblings (~10 files).

## Current vs. Scalable

**Current:**
```
internal/agent/session/
├── context_manager.go
├── context_pipeline.go
├── context_strategy.go
├── context_transformers.go     ← ~500 LOC
├── pruner.go
├── gatekeeper.go
├── token_counter.go
├── pipeline_factory.go
├── ui_bridge*.go               (5 files — already decomposed per ADR-025)
├── session_manager.go
├── skill_injector.go
├── injector.go
├── config_watcher.go
├── internal_tools.go
├── interfaces.go
└── ... (26 _test.go files)
```

**Scalable:**
```
internal/agent/session/
├── context/
│   ├── manager.go
│   ├── pipeline.go
│   ├── strategy.go
│   ├── transformers.go
│   ├── pruner.go
│   ├── gatekeeper.go
│   ├── token_counter.go
│   ├── pipeline_factory.go
│   └── *_test.go
├── ui_bridge*.go
├── session_manager.go
├── skill_injector.go
├── ...
```

## Proposed Action

1. **Pre-flight**: Run `find_usages` on every exported symbol in the 8 in-scope files. Confirm consumers are limited to `session_manager.go` and `agent.go`.
2. **Move files** using `git mv` to preserve history; update package declarations to `package context`.
3. **Identify back-references**: any symbol in the new `context/` package that needs a type from the parent `session` package indicates a misplaced concern — resolve before merging.
4. **Update `agent.go`**: replace `session.ContextManager` with `context.Manager`, `session.NewContextManager` with `context.NewManager`, etc.
5. **Verify `interfaces.go`**: any interface implemented by both `context/` and `session/` types must move to a neutral location (likely `domain/ports`).
6. Run full test suite with `-race` and `verify_architecture` to confirm no new layer violations.

## Acceptance Criteria

- [ ] `internal/agent/session/` production file count drops from 29 to ~21.
- [ ] New package `internal/agent/session/context/` exists with 8 production files + tests.
- [ ] `verify_architecture` reports no new violations.
- [ ] No circular dependency between `session/` and `session/context/`.
- [ ] All tests pass with `-race`.
- [ ] `golangci-lint run` clean.
- [ ] Public API of `agent` unchanged (verified via `get_file_skeleton internal/agent/agent.go` before/after).

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| `context_transformers.go` (~500 LOC) may have hidden coupling to `session_manager` internals | Audit with `find_usages` BEFORE moving; if coupling exists, file blocker sub-issue first |
| Test fixtures in `test_helpers_test.go` may be needed by both packages | Promote shared fixtures to `internal/pkg/testfixtures` |
| Renaming `session.ContextManager` → `context.Manager` is a 50+ site rename | Use `rename_symbol` tool, not manual sed |

## Out of Scope

- Extracting `skill_injector.go` / `injector.go` (separate future issue).
- Extracting `config_watcher.go`.
- Refactoring the contents of `context_transformers.go` (large file is a separate concern).

## Effort
**Medium** (3–5 days). Bulk is mechanical movement; risk lives in the back-reference audit.

## ADR Required
**Yes — ADR for sub-package decomposition pattern.** This is the first time `session/` is being split into sub-packages; the decision sets precedent for future extractions (`uibridge/`, `skills/`). Proposed: ADR-026 "Session Package Decomposition Strategy".

## References
- ADR-008 (`2026-01-domain-decomposition-strategy.md`) — package decomposition principles
- ADR-025 (`2026-04-uibridge-decomposition.md`) — prior intra-file decomposition (sets pattern for further extraction)
