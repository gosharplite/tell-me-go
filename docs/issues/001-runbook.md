# 🛠 Operational Runbook (Coder Execution Brief)

> This comment is a mechanical, step-by-step execution plan for the refactor specified in the issue body above. Read the issue body first for context, evidence, and acceptance criteria. This runbook tells you **how** to execute, not **why**.
>
> You have no prior memory of the analysis that produced this issue. Everything you need is either in this issue or discoverable via the commands below. If anything contradicts the issue body, **the issue body wins** — stop and report back.

---

## Phase 0 — Environment Verification

Run these checks. If any fails, **stop and report back** instead of guessing:

```bash
gh --version
gh auth status
gh repo view --json nameWithOwner    # must be gosharplite/tell-me-go
git status                           # working tree must be clean
git branch --show-current            # note current branch
go version                           # must be ≥ 1.26.2
```

---

## Phase 1 — Working Branch

```bash
git checkout main
git pull --ff-only
git checkout -b refactor/remove-agent-accessors
```

---

## Phase 2 — Audit `Get*` Accessors

The issue body confirmed all `Set*` callers are tests. You must independently audit the `Get*` methods, because the issue marks them as TBD.

### Step 2.1 — Run usage searches

For each method below, find every caller:

```bash
for method in GetCtxManager GetEvents GetConfigWatcher GetTracker GetRuntimeConfig; do
  echo "=== $method ==="
  grep -rn "\.${method}(" --include="*.go" . | grep -v "^./internal/agent/agent.go:"
done
```

### Step 2.2 — Apply the decision rule

| Caller location | Decision |
|---|---|
| Only in `*_test.go` files | **REMOVE** in this task |
| Any production file (non-`_test.go`) | **KEEP** in this task; record the production caller |

**Do not refactor production callers in this task — that is explicitly out of scope per the issue body.**

### Step 2.3 — Document audit results

Create `docs/issues/001-audit-results.md` with this exact format:

```markdown
# Get*/Set* Accessor Audit Results

Generated: <YYYY-MM-DD>
Branch: refactor/remove-agent-accessors

## Set* Methods (pre-verified by issue analysis)

| Method | Production callers | Test callers | Decision |
|---|---|---|---|
| SetCtxManager | none | 1 (agent_error_test.go) | REMOVE |
| SetEvents | none | 5 | REMOVE |
| SetConfigWatcher | none | 3 | REMOVE |
| SetTracker | none | 3 | REMOVE |
| SetLogger | none | 3 | REMOVE |
| SetRuntimeConfig | none | 2 | REMOVE |

## Get* Methods (audited in this task)

| Method | Production callers | Test callers | Decision |
|---|---|---|---|
| GetCtxManager | <list files or "none"> | <count> | REMOVE / KEEP |
| GetEvents | ... | ... | ... |
| GetConfigWatcher | ... | ... | ... |
| GetTracker | ... | ... | ... |
| GetRuntimeConfig | ... | ... | ... |
```

Commit this file before continuing:

```bash
git add docs/issues/001-audit-results.md
git commit -m "docs(agent): audit Get*/Set* accessor callers (refs #86)"
```

---

## Phase 3 — Verify or Add Required `AgentOption` Functions

The builder you'll create in Phase 4 must use existing `AgentOption` functions. Check what already exists:

```bash
grep -l "AgentOption" internal/agent/*.go
grep -n "^func With" internal/agent/options.go 2>/dev/null || \
  grep -rn "^func With.*AgentOption" internal/agent/ --include="*.go"
```

For each `Set*` method scheduled for REMOVE in your audit, confirm a matching `With*` `AgentOption` exists. If one is missing (likely candidates: `WithCtxManager`, `WithConfigWatcher`, `WithRuntimeConfig`), add it to the same file where the existing options live. Pattern:

```go
// WithCtxManager injects a pre-built ContextManager. Used primarily by tests
// that need to substitute a mock implementation.
func WithCtxManager(cm *session.ContextManager) AgentOption {
    return func(a *agent) {
        a.ctxManager = cm
    }
}
```

**Rules:**

- Match the naming, doc-comment style, and signature shape of existing `With*` functions exactly.
- Do not change any existing `AgentOption` function signature.
- This is the **only** production-code addition allowed in this task.

After adding any new options, run:

```bash
go build ./...
go vet ./...
```

Commit:

```bash
git add internal/agent/<file>.go
git commit -m "feat(agent): add AgentOption constructors needed for test builder (refs #86)"
```

---

## Phase 4 — Build the Test Helper

### Step 4.1 — Inspect the existing `agenttest` package

```bash
ls internal/agent/agenttest/
cat internal/agent/agenttest/doc.go 2>/dev/null
```

Match its conventions (file header, package comment style, import grouping).

### Step 4.2 — Create `internal/agent/agenttest/builder.go`

Required shape (adapt imports/types to actual package paths in this repo):

```go
// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
    "testing"

    "github.com/gosharplite/tell-me-go/internal/agent"
    "github.com/gosharplite/tell-me-go/internal/agent/session"
    "github.com/gosharplite/tell-me-go/internal/domain/events"
    "github.com/gosharplite/tell-me-go/internal/domain/llm"
    "github.com/gosharplite/tell-me-go/internal/domain/ports"
    "github.com/gosharplite/tell-me-go/internal/domain/pricing"
    "github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// AgentBuilder constructs *agent.Agent values for tests using composition
// instead of post-construction mutation. It exists to eliminate the need for
// Set*/Get* accessors on the production agent type.
//
// Usage:
//
//   a := agenttest.NewAgentBuilder(t).
//       WithGateway(mockGateway).
//       WithEventBus(mockBus).
//       WithRegistry(mockRegistry).
//       WithLogger(mockLogger).
//       Build()
type AgentBuilder struct {
    t       testing.TB
    gateway llm.LLMGateway
    events  events.EventBus
    reg     tools.Registry
    opts    []agent.AgentOption
}

// NewAgentBuilder returns a builder bound to the given test. Build() will call
// t.Fatal on any construction error.
func NewAgentBuilder(t testing.TB) *AgentBuilder {
    t.Helper()
    return &AgentBuilder{t: t}
}

// --- Required dependencies (replace constructor positional args) ---

func (b *AgentBuilder) WithGateway(gw llm.LLMGateway) *AgentBuilder {
    b.gateway = gw
    return b
}

func (b *AgentBuilder) WithEventBus(bus events.EventBus) *AgentBuilder {
    b.events = bus
    b.opts = append(b.opts, agent.WithEvents(bus)) // if such an option exists
    return b
}

func (b *AgentBuilder) WithRegistry(r tools.Registry) *AgentBuilder {
    b.reg = r
    return b
}

// --- Optional dependencies (one method per Set* being removed) ---

func (b *AgentBuilder) WithLogger(l ports.Logger) *AgentBuilder {
    b.opts = append(b.opts, agent.WithLogger(l))
    return b
}

func (b *AgentBuilder) WithTracker(tr pricing.CostTracker) *AgentBuilder {
    b.opts = append(b.opts, agent.WithSessionCostTracker(tr))
    return b
}

func (b *AgentBuilder) WithConfigWatcher(cw session.ConfigWatcher) *AgentBuilder {
    b.opts = append(b.opts, agent.WithConfigWatcher(cw))
    return b
}

func (b *AgentBuilder) WithCtxManager(cm *session.ContextManager) *AgentBuilder {
    b.opts = append(b.opts, agent.WithCtxManager(cm))
    return b
}

// ... add one With* method per Set* method your audit decided to REMOVE.

// Build constructs the agent and fails the test on error.
func (b *AgentBuilder) Build() *agent.Agent {
    b.t.Helper()
    if b.gateway == nil {
        b.t.Fatal("agenttest: AgentBuilder requires WithGateway")
    }
    if b.events == nil {
        b.t.Fatal("agenttest: AgentBuilder requires WithEventBus")
    }
    if b.reg == nil {
        b.t.Fatal("agenttest: AgentBuilder requires WithRegistry")
    }
    a, err := agent.NewAgent(b.gateway, b.events, b.reg, b.opts...)
    if err != nil {
        b.t.Fatalf("agenttest: AgentBuilder.Build: %v", err)
    }
    return a
}
```

**Critical design rules — do not violate:**

1. Each `With*` method **must** internally append the existing `agent.AgentOption` function. No parallel mechanism.
2. The builder must **never** mutate the agent after `Build()` returns.
3. `Build()` must call `t.Helper()` and `t.Fatal(err)` on error.
4. **Verify the actual return type** of `agent.NewAgent` — it may be an interface or pointer; match it precisely. The skeleton above guesses `*agent.Agent`; correct as needed via:
   ```bash
   grep -n "^func NewAgent" internal/agent/agent.go
   ```

### Step 4.3 — Add builder unit test

Create `internal/agent/agenttest/builder_test.go` covering:

- **Minimal build**: only required dependencies → succeeds.
- **All-options build**: every `With*` method invoked → succeeds.
- **Missing required dependency**: omitting `WithGateway` (or `WithEventBus`, `WithRegistry`) → uses `testing.T` substitute that records `Fatal` calls (or use `testing.T.Run` with a sub-test that's expected to fail; see existing patterns in the repo for how unit tests verify `t.Fatal` invocation).

If the repo already has a pattern for testing `t.Fatal` (look for `mockT` or similar in existing tests), match it. If not, the simplest approach is to mark the negative-path test as documenting the contract via a comment and skip the actual Fatal-trigger.

### Step 4.4 — Verify and commit

```bash
go build ./...
go test ./internal/agent/agenttest/... -race -count=1
git add internal/agent/agenttest/
git commit -m "test(agent): add AgentBuilder for test composition (refs #86)"
```

---

## Phase 5 — Migrate Test Callers (One Commit Per File)

### Step 5.1 — Find all call sites

```bash
grep -rn "\.SetCtxManager\|\.SetEvents\|\.SetConfigWatcher\|\.SetTracker\|\.SetLogger\|\.SetRuntimeConfig" --include="*_test.go" .
```

Expected files (verify against your grep output):

- `internal/agent/agent_error_test.go`
- `internal/agent/agent_lifecycle_test.go`
- `internal/agent/session/context_manager_test.go` (only `SetLogger`)
- `tests/integration/agent/agent_integration_test.go`

### Step 5.2 — Per-file migration loop

For **each** file in the list, repeat:

1. **Read** the file fully. Identify each `agent.NewAgent(...)` + `Set*(...)` sequence.
2. **Replace** the sequence with a single `agenttest.NewAgentBuilder(t).With*(...).Build()` chain. Order the `With*` calls to match the original `Set*` call order, for review legibility.
3. **Run only that test file** with race detection:
   ```bash
   go test -race -count=1 -run . <package_path>
   # e.g. go test -race -count=1 -run . ./internal/agent/...
   ```
   If it fails, **stop** — fix before moving on. Do not touch the next file with a broken state.
4. **Commit** that single file:
   ```bash
   git add <file>
   git commit -m "test(agent): migrate <basename> to agenttest.AgentBuilder

   Refs #86"
   ```

**One commit per test file.** This makes review tractable and bisect-friendly.

---

## Phase 6 — Delete the Accessor Methods

Only after **all** test migrations compile and pass.

### Step 6.1 — Locate the methods

```bash
grep -n "^func (a \*agent) \(Get\|Set\)" internal/agent/agent.go
```

### Step 6.2 — Remove

For each method your audit marked **REMOVE**:

- Delete the method declaration (the entire `func` block including its doc comment).
- **Keep the corresponding struct field** — it's still assigned by the `AgentOption` constructors.
- Use precise editing (read the method's exact byte range, replace with empty). Do not regex-delete — risk of catching unintended methods.

### Step 6.3 — Verify

```bash
go build ./...
go vet ./...
grep -c "^func (a \*agent)" internal/agent/agent.go
# Expected: was 20, now 20 minus (count of removed methods)
# Acceptance criterion target: ≤ 12
```

If the count exceeds 12 after this step, your audit kept too many methods due to production callers. That's acceptable — record it in the PR description, but do not force removal of methods with production callers.

### Step 6.4 — Commit

```bash
git add internal/agent/agent.go
git commit -m "refactor(agent): remove Get*/Set* test-only accessors (refs #86)

Removed methods (test-only after Phase 5 migration):
- <list each method>

Methods kept (production callers):
- <list each method + production caller, or 'none'>"
```

---

## Phase 7 — Verification Gauntlet

Run in this exact order. **Stop and report at the first failure** — do not auto-fix without checking in:

```bash
# 1. Build
go build ./...

# 2. Vet
go vet ./...

# 3. Full test suite with race detector
go test ./... -race -count=1

# 4. Lint (skip and note in PR if not installed)
golangci-lint run

# 5. Structural acceptance criteria
echo "=== Method count on agent struct ==="
grep -c "^func (a \*agent)" internal/agent/agent.go
# Target: ≤ 12

echo "=== Remaining Set* references in tests ==="
grep -rn "\.SetCtxManager\|\.SetEvents\|\.SetConfigWatcher\|\.SetTracker\|\.SetLogger\|\.SetRuntimeConfig" --include="*.go" . || echo "✓ none remaining"

echo "=== Field additions to agent struct ==="
git diff main -- internal/agent/agent.go | grep -E "^\+\s+[a-zA-Z_]+\s+[a-zA-Z*]" | grep -v "^+++" || echo "✓ no field additions"

echo "=== Architecture check ==="
# Optional but recommended: any tool the repo uses for layer verification
```

---

## Phase 8 — Open the PR

```bash
git push -u origin refactor/remove-agent-accessors

gh pr create \
  --repo gosharplite/tell-me-go \
  --title "refactor(agent): remove Get*/Set* test-only accessors (closes #86)" \
  --body "$(cat <<'EOF'
Implements the refactor specified in #86.

## Audit Results
See `docs/issues/001-audit-results.md` (committed in this branch).

## Methods Removed
<list each removed method>

## Methods Kept (with reason)
<list each kept method and the production caller blocking removal — or "none, all test-only methods removed">

## New AgentOption Constructors Added
<list any new With* options added in Phase 3 — or "none, all needed options pre-existed">

## Verification
- [ ] `go build ./...` — clean
- [ ] `go vet ./...` — clean
- [ ] `go test ./... -race -count=1` — pass
- [ ] `golangci-lint run` — clean (or note: not installed)
- [ ] `agent.go` method count: 20 → <N>
- [ ] No new fields added to agent struct
- [ ] No remaining Set* references in tests

## Out of Scope (per #86)
- Splitting the 23-field agent struct into composed layers
- Modifying existing AgentOption signatures
- Modifying any file under internal/agent/session/ except the one known test file
- Modifying any file under internal/infrastructure/

## Commit Trail
This PR uses one commit per logical step for bisect-friendliness:
1. Audit results
2. New AgentOption constructors (if any)
3. AgentBuilder + tests
4. One commit per migrated test file
5. Accessor method deletion

Closes #86
EOF
)"
```

---

## 🚫 Hard Constraints (Repeated for Emphasis)

1. **Do not** modify any file under `internal/agent/session/` except the one known test file (`session/context_manager_test.go`). Session restructuring is **issue #88**.
2. **Do not** modify any file under `internal/infrastructure/`. DI restructuring is **issue #87**.
3. **Do not** add new exported types or functions to the `agent` package beyond the small set of `AgentOption` functions identified in Phase 3.
4. **Do not** introduce new dependencies in `go.mod`.
5. **Do not** remove `agent` struct fields, even if their setter is gone — `AgentOption` constructors still assign them.
6. **Do not** "improve" anything noticed in passing — if you spot something, append it to a `FOLLOWUPS.md` file at repo root and keep moving.
7. **Do not** force-push or rewrite history once the PR is open. Add fixup commits if review feedback requires changes.

---

## 📋 Final Report Template

When done (or blocked), comment back on this issue with:

```markdown
## Execution Report — Issue #86

**Status:** Complete / Blocked / Partial
**PR:** #<N> — <url>
**Branch:** refactor/remove-agent-accessors
**Audit results:** docs/issues/001-audit-results.md

### Verification Output
<paste the output of Phase 7's gauntlet>

### Deviations from Runbook
<list any, with justification — or "none">

### Items added to FOLLOWUPS.md
<list any, with one-line description — or "none">

### Blockers (if Status != Complete)
<describe what stopped progress and what decision is needed>
```
