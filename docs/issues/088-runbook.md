# 🛠 Operational Runbook (Coder Execution Brief)

> This comment is a mechanical, step-by-step execution plan for the refactor specified in the issue body above. Read the issue body **and ADR-026** (`docs/adr/2026-04-session-context-subpackage-extraction.md`) before starting. The ADR contains the design rationale and coupling-hazard resolutions. This runbook tells you **how** to execute, not **why**.
>
> You have no prior memory of the analysis that produced this issue. Everything you need is in this issue, in ADR-026, or discoverable via the commands below. If anything contradicts the ADR, **the ADR wins** — stop and report back.

---

## ⚠ Prerequisites

**Do not start this task until both are true:**

1. **Issue #86 is merged.** Check with: `gh pr list --repo gosharplite/tell-me-go --search "closed:>2026-04-29 closes #86"`
2. **ADR-026 is on `main`.** Check with: `git fetch origin main && git show origin/main:docs/adr/2026-04-session-context-subpackage-extraction.md | head -10`

If either is false, **stop and report back**. Do not proceed without these prerequisites — Issue #86's accessor cleanup may simplify the agent.go consumer-update step in Phase 7, and ADR-026 is the authoritative design spec.

---

## Phase 0 — Environment Verification

```bash
gh --version
gh auth status
gh repo view --json nameWithOwner    # must be gosharplite/tell-me-go
git status                           # working tree must be clean
git checkout main
git pull --ff-only
go version                           # must be ≥ 1.26.2
```

If any check fails, **stop and report back**.

---

## Phase 1 — Working Branch and ADR Status

```bash
git checkout -b refactor/extract-session-context

# Confirm ADR-026 is present on this branch
ls docs/adr/2026-04-session-context-subpackage-extraction.md

# If ADR status is still "Proposed", flip to "Accepted" as the first commit
sed -i.bak 's/^## Status$/## Status/;s/^Proposed$/Accepted/' \
  docs/adr/2026-04-session-context-subpackage-extraction.md
rm docs/adr/2026-04-session-context-subpackage-extraction.md.bak

# Verify the change took effect
grep -A1 "^## Status" docs/adr/2026-04-session-context-subpackage-extraction.md
# Expected output:
#   ## Status
#   Accepted

git add docs/adr/2026-04-session-context-subpackage-extraction.md
git commit -m "docs(adr): accept ADR-026 (session/context extraction)

Refs #88"
```

---

## Phase 2 — Pre-Flight Audit

These commands establish a baseline you'll compare against in the verification phase. **Capture all output to a file** for the PR description.

```bash
mkdir -p /tmp/issue88
{
  echo "=== Pre-flight audit for Issue #88 ==="
  echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "Commit: $(git rev-parse HEAD)"
  echo

  echo "--- Production file LOC in session/ ---"
  ls internal/agent/session/*.go | grep -v _test.go | xargs wc -l | sort -rn

  echo
  echo "--- Test file count in session/ ---"
  ls internal/agent/session/*_test.go | wc -l

  echo
  echo "--- External callers of types being moved ---"
  for sym in ContextManager ContextStrategy PipelineFactory NewContextManager NewContextStrategy NewPipelineFactory NewHeuristicTokenCounter HeuristicTokenCounter TokenGatekeeper HistoryPruner WarningInjector; do
    echo "## $sym"
    grep -rn "session\.${sym}\b" --include="*.go" . | grep -v "_test.go" || echo "  (none in production code)"
    echo
  done

  echo "--- Tests in session/ that touch types being moved ---"
  for sym in ContextManager ContextStrategy PipelineFactory; do
    echo "## $sym (test usage count by file)"
    grep -l "\b${sym}\b" internal/agent/session/*_test.go | sort -u
    echo
  done
} > /tmp/issue88/preflight.txt

cat /tmp/issue88/preflight.txt
```

**Expected production callers (from ADR-026 §Consumer Update Surface):**

- `internal/agent/agent.go` (~5 lines)
- `internal/agent/session/session_manager.go` (~8 lines)
- `internal/agent/session/internal_tools.go` (~3 lines)
- `internal/agent/session/skill_injector.go` (~2 lines)

**If your audit finds callers outside this list, stop and report back** — the design may need amendment, which requires updating ADR-026 first.

Commit the audit:

```bash
mkdir -p docs/issues
cp /tmp/issue88/preflight.txt docs/issues/088-preflight-audit.txt
git add docs/issues/088-preflight-audit.txt
git commit -m "docs(issue-88): pre-flight audit baseline

Refs #88"
```

---

## Phase 3 — Create the New Package Skeleton

```bash
mkdir -p internal/agent/session/context

cat > internal/agent/session/context/doc.go <<'EOF'
// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

/*
Package context implements the context preparation pipeline for the chat session.

It encapsulates the construction, validation, and pruning of LLM input context
through a chain of ports.ContextTransformer instances. The package is consumed
by the parent session package and by internal/agent.

Entry points:

  - Manager       — orchestrates the pipeline; primary façade.
  - Strategy      — token-budget and limits accounting.
  - Factory       — builds a standard transformer pipeline for a given limits set.

This package does not depend on any sibling under internal/agent/session.
The reverse direction (session → session/context) is the only allowed coupling,
maintained explicitly by session/internal_tools.go and session/skill_injector.go.

See ADR-026 for the decomposition rationale.
*/
package context
EOF

git add internal/agent/session/context/doc.go
git commit -m "feat(session/context): scaffold sub-package with doc.go

Refs #88"
```

---

## Phase 4 — Move Files (One Pair Per Commit)

For **each** of the 9 production files plus its test siblings, follow the move sequence below. **Do not batch moves** — one commit per file pair makes review and `git bisect` tractable.

### Per-file move sequence

The mapping (from ADR-026 §Files Moved):

| # | Old path | New path |
|---|---|---|
| 1 | `session/token_counter.go` | `context/token_counter.go` |
| 2 | `session/context_strategy.go` | `context/strategy.go` |
| 3 | `session/context_pipeline.go` | `context/pipeline.go` |
| 4 | `session/context_transformers.go` | `context/transformers.go` |
| 5 | `session/pruner.go` | `context/pruner.go` |
| 6 | `session/gatekeeper.go` | `context/gatekeeper.go` |
| 7 | `session/injector.go` | `context/warning_injector.go` |
| 8 | `session/pipeline_factory.go` | `context/pipeline_factory.go` |
| 9 | `session/context_manager.go` | `context/manager.go` |

**Order matters** — move the leaves first (token_counter, strategy) so their consumers can be fixed up incrementally. Manager is moved **last**.

For each row:

```bash
# 1. Move file with git (preserves blame history)
git mv <old_path> <new_path>

# 2. Find and move corresponding test files
for tf in internal/agent/session/<base_name>_*_test.go internal/agent/session/<base_name>_test.go; do
  [ -f "$tf" ] && git mv "$tf" "internal/agent/session/context/$(basename "$tf")"
done

# 3. Update the package declaration in the moved files
sed -i.bak 's/^package session$/package context/' \
  internal/agent/session/context/<new_basename>*.go
rm internal/agent/session/context/*.bak

# 4. The build WILL be broken at this point. That is expected.
#    Do NOT attempt go build yet. Continue to Phase 5 within the same logical commit.

git status
```

**STOP** after step 4 of the **first** file (token_counter.go). Do not commit yet — first complete the rename within that file (next phase). Then commit the rename + move together as one unit per file.

### Special handling: the `injector.go → warning_injector.go` rename

For file #7, the filename changes (per ADR-026 — it currently obscures the type's purpose). After `git mv`, also verify the file contains only `WarningInjector` and not `skillInjector`:

```bash
grep -l "WarningInjector\|skillInjector" internal/agent/session/context/warning_injector.go internal/agent/session/skill_injector.go
# Expected:
#   internal/agent/session/context/warning_injector.go    (WarningInjector only)
#   internal/agent/session/skill_injector.go              (skillInjector only)
```

---

## Phase 5 — Type Renames Within Each Moved File

Per ADR-026 §Renaming Convention, three exported types are renamed to remove the now-redundant `Context*` prefix:

| Old name | New name | Tool |
|---|---|---|
| `ContextManager` | `Manager` | `rename_symbol` |
| `ContextStrategy` | `Strategy` | `rename_symbol` |
| `PipelineFactory` | `Factory` | `rename_symbol` |
| `NewContextManager` | `NewManager` | `rename_symbol` |
| `NewContextStrategy` | `NewStrategy` | `rename_symbol` |
| `NewPipelineFactory` | `NewFactory` | `rename_symbol` |

**Use AST-based renaming, not `sed`.** Project tooling provides `rename_symbol` (or use `gopls rename` if invoking Go tooling directly). For each rename:

```bash
# Example for ContextManager → Manager
# (use the project's preferred AST refactoring tool)
gopls -remote=auto rename -w internal/agent/session/context/manager.go:<line>:<col> Manager
```

**Constraint:** Renames must update **all** references across the entire codebase atomically per symbol. Verify after each rename:

```bash
go build ./...
# Expect failures only in consumer files (agent.go, session_manager.go, internal_tools.go, skill_injector.go)
# AT THIS PHASE — those are fixed in Phase 7. All OTHER packages must build clean.
```

If unrelated packages fail to build, **stop and report back** — the rename hit something the audit missed.

---

## Phase 6 — Adjust the `PipelineFactory` Signature

Per ADR-026 §Coupling Hazard Resolutions §2, `Factory.BuildStandardPipeline` (formerly `PipelineFactory.BuildStandardPipeline`) must accept `extras ...ports.ContextTransformer` so the parent `session/` package can inject `skillInjector` without creating a `context/ → domain/skills` import.

### Step 6.1 — Modify the factory

In `internal/agent/session/context/pipeline_factory.go`:

```go
// BEFORE
func (f *Factory) BuildStandardPipeline(limits events.Limits) *pipeline {
    // ... existing body that constructs all transformers including skillInjector
}

// AFTER
func (f *Factory) BuildStandardPipeline(
    limits events.Limits,
    extras ...ports.ContextTransformer,
) *pipeline {
    // ... existing body, but REMOVE skillInjector construction
    // ... at the appropriate ordering point, append extras to the transformer slice
}
```

**Critical:**

- Do **not** change the order of existing transformers.
- The `extras` slice is appended at the position where `skillInjector` used to be constructed (preserve runtime ordering).
- Remove the `skillInjector` construction from the factory body — it now comes in as an extra.
- Do **not** remove the `skillInjector` type itself; it stays in `session/skill_injector.go`.

### Step 6.2 — Verify ordering preservation

Add a regression test in `internal/agent/session/context/pipeline_factory_test.go` (extend the existing `TestPipelineFactory_BuildStandardPipeline_PrunerInclusion` or add a new one):

```go
func TestFactory_BuildStandardPipeline_ExtraTransformerOrdering(t *testing.T) {
    // Construct factory with minimal deps
    f := NewFactory(/* ... */)
    extra := &mockTransformer{priority: <position of skillInjector before refactor>}
    p := f.BuildStandardPipeline(events.Limits{}, extra)

    // Assert the extra is present in the pipeline at the expected position
    // (use whatever inspection method the existing pruner-inclusion test uses)
}
```

Commit:

```bash
git add internal/agent/session/context/pipeline_factory.go \
        internal/agent/session/context/pipeline_factory_test.go
git commit -m "refactor(session/context): accept extras in Factory.BuildStandardPipeline

Implements the coupling-hazard resolution from ADR-026 §2.
Allows parent session package to inject skillInjector without creating
a context → domain/skills import.

Refs #88"
```

---

## Phase 7 — Update Consumers

Update the four consumer files identified by the pre-flight audit. **One commit per consumer file** for bisect-friendliness.

### Consumer 1: `internal/agent/agent.go`

```bash
# Add the import
# (use AST-aware editing or your editor's organize-imports — do NOT hand-edit imports if avoidable)

# Required edits:
#   - import sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
#   - replace session.ContextManager → sessctx.Manager
#   - any other context-type references identified in the audit

go build ./...   # must succeed for internal/agent/...
go vet ./internal/agent/...

git add internal/agent/agent.go
git commit -m "refactor(agent): migrate to session/context sub-package

Refs #88"
```

### Consumer 2: `internal/agent/session/session_manager.go`

This file is the most complex consumer because it constructs the full pipeline. Required edits:

- Add `sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"` import.
- Replace constructor calls:
  - `NewContextStrategy(...)` → `sessctx.NewStrategy(...)`
  - `NewPipelineFactory(...)` → `sessctx.NewFactory(...)`
  - `NewContextManager(...)` → `sessctx.NewManager(...)`
  - `NewHeuristicTokenCounter(...)` → `sessctx.NewHeuristicTokenCounter(...)`
- Replace type references for `*ContextManager`, `*ContextStrategy`, `*PipelineFactory` with their `sessctx.*` equivalents.
- Update the `BuildStandardPipeline` call to pass `skillInjector` as an extra:

  ```go
  // BEFORE
  pipeline := factory.BuildStandardPipeline(limits)

  // AFTER
  pipeline := factory.BuildStandardPipeline(limits, skillInjector)
  ```

```bash
go build ./...
go vet ./internal/agent/session/...

git add internal/agent/session/session_manager.go
git commit -m "refactor(session): migrate session_manager to context sub-package

- Imports session/context as sessctx
- Renames ContextManager → Manager, ContextStrategy → Strategy, PipelineFactory → Factory
- Passes skillInjector as extra to BuildStandardPipeline (ADR-026 §2)

Refs #88"
```

### Consumer 3: `internal/agent/session/internal_tools.go`

```bash
# Edits:
#   - import sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
#   - NewInternalTools(cm *ContextManager)   →   NewInternalTools(cm *sessctx.Manager)
#   - RegisterInternal(r tools.ToolRegistrar, cm *ContextManager) →
#     RegisterInternal(r tools.ToolRegistrar, cm *sessctx.Manager)

go build ./...
go vet ./internal/agent/session/...

git add internal/agent/session/internal_tools.go
git commit -m "refactor(session): migrate internal_tools to context sub-package

Refs #88"
```

### Consumer 4: `internal/agent/session/skill_injector.go`

This file implements `ports.ContextTransformer`, so the interface contract is unchanged — the only edit should be removing any direct reference to the moved factory if one exists. Per the audit it should be ~2 lines.

```bash
go build ./...
go vet ./internal/agent/session/...

git add internal/agent/session/skill_injector.go
git commit -m "refactor(session): adjust skill_injector for context sub-package

Refs #88"
```

---

## Phase 8 — Test Migration

The test files moved in Phase 4 may reference test helpers that stayed in `session/`. Diagnose:

```bash
go test ./internal/agent/session/context/... 2>&1 | head -50
```

Common issues and fixes:

| Symptom | Fix |
|---|---|
| `undefined: syncWriter` | Helper from `test_helpers_test.go` is needed in both packages. Copy the helper into `internal/agent/session/context/test_helpers_test.go` (NOT promote to `internal/pkg/testfixtures` per ADR-026 §3). |
| `undefined: AsSessionManagerInternal` | Test relied on `export_test.go`. If the test exercises only context types, refactor it to use the now-exported (within-package) types directly. If it genuinely needs session internals, **the test belongs in `session/`, not `context/`** — move it back. |
| Test imports `internal/agent/session` | Should now import `internal/agent/session/context` for context types. Update. |

Commit each fix as a separate small commit:

```bash
git add internal/agent/session/context/<fixed_test_file>.go
git commit -m "test(session/context): <one-line fix description>

Refs #88"
```

If you find a test that genuinely needs **both** `session` and `session/context` types, that's a sign the test is at the wrong layer — leave it in `session/` and update its imports rather than moving it to `context/`.

---

## Phase 9 — Verification Gauntlet

Run in this exact order. **Stop and report at the first failure** — do not auto-fix without checking in:

```bash
# 1. Build
go build ./...

# 2. Vet
go vet ./...

# 3. Race-detector tests (full suite)
go test ./... -race -count=1

# 4. Lint (skip and note in PR if not installed)
golangci-lint run

# 5. Architecture verification
# Run whatever architecture-verification tool the repo uses; expect: clean.

# 6. Structural acceptance criteria
{
  echo "=== Post-refactor structural metrics ==="
  echo
  echo "--- session/ production file count (target: ≤ 21, was 29) ---"
  ls internal/agent/session/*.go | grep -v _test.go | grep -v context/ | wc -l

  echo
  echo "--- session/context/ production file count (target: 9) ---"
  ls internal/agent/session/context/*.go | grep -v _test.go | wc -l

  echo
  echo "--- session/ production LOC (target: dropped by ~2,200) ---"
  ls internal/agent/session/*.go | grep -v _test.go | grep -v context/ | xargs wc -l | tail -1

  echo
  echo "--- session/context/ production LOC (target: ~2,200) ---"
  ls internal/agent/session/context/*.go | grep -v _test.go | xargs wc -l | tail -1

  echo
  echo "--- Verify no session/context/ imports session/ ---"
  grep -rn "tell-me-go/internal/agent/session\"" internal/agent/session/context/ \
    && echo "✗ VIOLATION — context/ must not import session/" \
    || echo "✓ no upward imports"

  echo
  echo "--- Verify session → session/context import is the only new edge ---"
  grep -rn "tell-me-go/internal/agent/session/context" internal/agent/session/ --include="*.go" | grep -v "context/"
} > /tmp/issue88/postflight.txt

cat /tmp/issue88/postflight.txt
cp /tmp/issue88/postflight.txt docs/issues/088-postflight-metrics.txt
git add docs/issues/088-postflight-metrics.txt
git commit -m "docs(issue-88): post-refactor structural metrics

Refs #88"
```

---

## Phase 10 — Open the PR

```bash
git push -u origin refactor/extract-session-context

gh pr create \
  --repo gosharplite/tell-me-go \
  --title "refactor(session): extract session/context sub-package (closes #88)" \
  --body "$(cat <<'EOF'
Implements the refactor specified in #88 per ADR-026.

## Design Reference
docs/adr/2026-04-session-context-subpackage-extraction.md (status: Accepted)

## Pre-flight & Post-flight Metrics
- `docs/issues/088-preflight-audit.txt`
- `docs/issues/088-postflight-metrics.txt`

## Files Moved
| Old | New |
|---|---|
| session/context_manager.go | session/context/manager.go |
| session/context_pipeline.go | session/context/pipeline.go |
| session/context_strategy.go | session/context/strategy.go |
| session/context_transformers.go | session/context/transformers.go |
| session/pruner.go | session/context/pruner.go |
| session/gatekeeper.go | session/context/gatekeeper.go |
| session/token_counter.go | session/context/token_counter.go |
| session/pipeline_factory.go | session/context/pipeline_factory.go |
| session/injector.go | session/context/warning_injector.go |
| (10 test files moved with siblings) | |

## Type Renames
- ContextManager → Manager
- ContextStrategy → Strategy
- PipelineFactory → Factory
- (and corresponding constructors)

## API Change
`Factory.BuildStandardPipeline` now accepts `extras ...ports.ContextTransformer` (ADR-026 §2). This breaks the previous `PipelineFactory.BuildStandardPipeline` signature, but the only caller is `session/session_manager.go`, updated in this PR.

## Verification
- [ ] `go build ./...` — clean
- [ ] `go vet ./...` — clean
- [ ] `go test ./... -race -count=1` — pass
- [ ] `golangci-lint run` — clean (or note: not installed)
- [ ] Architecture check — clean
- [ ] session/ production file count: 29 → <N>
- [ ] session/context/ production file count: <N>
- [ ] No upward imports (context/ → session/) — verified

## Out of Scope (per #88 and ADR-026)
- Extracting session/uibridge/ (deferred to follow-up ADR)
- Extracting session/skills/ (deferred)
- Extracting session/configwatch/ (deferred)
- Refactoring the contents of context_transformers.go (large file is a separate concern)
- Promoting test fixtures to internal/pkg/testfixtures (ADR-026 §3 — only on >50 LOC duplication)

## Commit Trail (bisect-friendly)
1. ADR-026 acceptance
2. Pre-flight audit
3. Sub-package scaffold (doc.go)
4. One commit per moved file pair (9 commits)
5. Factory signature change
6. One commit per consumer (4 commits)
7. Test migration fixes (variable count)
8. Post-flight metrics

Closes #88
EOF
)"
```

---

## 🚫 Hard Constraints (Repeated for Emphasis)

1. **Do not** start until Issue #86 is merged AND ADR-026 is on `main`.
2. **Do not** modify any file under `internal/infrastructure/`. DI restructuring is **issue #87**.
3. **Do not** refactor the contents of `context_transformers.go` — only move and rename. Internal cleanup is a separate future task.
4. **Do not** introduce new dependencies in `go.mod`.
5. **Do not** change runtime behaviour. This is purely structural.
6. **Do not** rename types other than the three identified in Phase 5. Other renames require ADR amendment.
7. **Do not** move `internal_tools.go`, `skill_injector.go`, `config_watcher.go`, or any `ui_bridge*.go` file. They stay in `session/` per ADR-026.
8. **Do not** "improve" anything noticed in passing — append to `FOLLOWUPS.md` at repo root and keep moving.
9. **Do not** force-push or rewrite history once the PR is open. Add fixup commits if review feedback requires changes.
10. **If the audit in Phase 2 finds production callers outside the four files listed in ADR-026 §Consumer Update Surface, STOP** — the design needs amendment via an ADR-026 update PR before this work can proceed.

---

## 📋 Final Report Template

When done (or blocked), comment back on this issue with:

```markdown
## Execution Report — Issue #88

**Status:** Complete / Blocked / Partial
**PR:** #<N> — <url>
**Branch:** refactor/extract-session-context
**ADR:** docs/adr/2026-04-session-context-subpackage-extraction.md (Accepted in commit <sha>)

### Pre-flight Audit
docs/issues/088-preflight-audit.txt
- Production callers found: <list> (matches ADR-026 §Consumer Update Surface? Y/N)

### Post-flight Metrics
docs/issues/088-postflight-metrics.txt
- session/ production files: 29 → <N>
- session/context/ production files: <N>
- Architecture check: clean / <issues>

### Verification Output
<paste the output of Phase 9's gauntlet>

### Deviations from Runbook
<list any, with justification — or "none">

### Items added to FOLLOWUPS.md
<list any, with one-line description — or "none">

### Blockers (if Status != Complete)
<describe what stopped progress and what decision is needed>
```
