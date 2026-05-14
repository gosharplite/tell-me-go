<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Multi-Agent Issue-to-PR Orchestration

### Objective
This SOP defines the end-to-end process for resolving a GitHub issue using the **tell-me-go** multi-agent loop (Architect + Coder), verifying the result with `make check-full`, and delivering a clean PR targeting the `dev` branch.

---

### Prerequisites
- **Read and understand** [`docs/steps/chatting-with-ai.md`](../../steps/chatting-with-ai.md) — the tell-me-go protocol. This SOP uses `write_file` → `mktemp` + `cp` → `tell-me-go` patterns, `--new` vs. continuation rules, `-l N` retrieval, `-t` transcript debugging, and the mandatory `timeout: 1800` requirement. You cannot follow this SOP without knowing those patterns.
- A GitHub issue with clear scope, acceptance criteria, and a Definition of Done.
- Two role config files exported as environment variables:
  ```bash
  export ARCHITECT_CONFIG=/path/to/architect.yaml
  export CODER_CONFIG=/path/to/coder.yaml
  ```
- The `tell-me-go` binary on `PATH`.
- The `gh` CLI authenticated (`gh auth status`).
- A clean working tree: `git status` must show no uncommitted changes.

---

### Roles and Boundaries

| Role | Access | Responsibility |
|------|--------|----------------|
| **Orchestrator** (you) | Read files, run commands, `tell-me-go` | Issue analysis, prompt writing, retrieval, verification, PR creation. **Never modifies Go source files.** |
| **Architect** | Read-only via `tell-me-go -r` | Structural analysis, risk assessment, implementation sequencing, `[ARCHITECTURAL BLOCKER]` / `[TECHNICAL DEBT]` / `[REFACTOR]` categorization. |
| **Coder** | **Write** via `tell-me-go -r` (the only role with file write access) | Implementation: TDD, table-driven tests, code changes, self-review. |

Communication is **asynchronous and turn-based**. The Orchestrator is the hub — Architect and Coder never talk directly.

---

### 1. Branch Creation

Before any work begins, create a feature branch from `dev`:

```bash
git checkout dev
git pull origin dev
git checkout -b fix/issue-<NUMBER>-<short-description>
```

Verify:
```bash
git branch --show-current
# Must output: fix/issue-<NUMBER>-<short-description>
```

---

### 2. Phase 1 — Architectural Analysis

#### 2a. Write the Architect prompt

Use `write_file` to create the prompt (never inline heredocs in `execute_command`):

```
write_file → /tmp/tell-me-go-prompt.txt
```

The prompt must include:
- The full issue body (scope, gap distribution, key code locations, DoD).
- A request for: risk assessment, prioritized implementation sequence, test strategy, and pattern guidance.
- An explicit instruction to categorize findings per the Architect SOP.

#### 2b. Send to Architect

```bash
PROMPT_FILE=$(mktemp) && \
cp /tmp/tell-me-go-prompt.txt "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go --new -r -c ${ARCHITECT_CONFIG} < "$PROMPT_FILE" &> /dev/null
```

> **CRITICAL**: Set `timeout: 1800` on every `tell-me-go` call. The default 15s limit will kill the sub-agent.

#### 2c. Retrieve the Architect's response

```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG}
```

Verify the response is actionable before proceeding. If the Architect asks a question, answer via a follow-up prompt (**no `--new`**) to preserve context.

---

### 3. Phase 2 — Implementation

#### 3a. Write the Coder prompt

```
write_file → /tmp/tell-me-go-prompt.txt
```

The prompt must include:
- The Architect's full prioritized sequence and test strategy.
- The specific code locations from the issue.
- A directive: implement one gap at a time, run tests after each, report progress.
- The Definition of Done from the issue.

#### 3b. Send to Coder (new session)

```bash
PROMPT_FILE=$(mktemp) && \
cp /tmp/tell-me-go-prompt.txt "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go --new -r -c ${CODER_CONFIG} < "$PROMPT_FILE" &> /dev/null
```

#### 3c. Retrieve the Coder's response

```bash
tell-me-go -l 1 -r -c ${CODER_CONFIG}
```

#### 3d. Handle mid-implementation questions

If the Coder asks a question or hits a blocker:

1. Read the question from the Coder's output.
2. **Option A**: Answer directly — `write_file` a follow-up, re-send with `tell-me-go -r -c ${CODER_CONFIG}` (no `--new`).
3. **Option B**: Escalate to Architect — `write_file` the question to Architect, send with `tell-me-go -r -c ${ARCHITECT_CONFIG}` (no `--new`), retrieve answer, forward to Coder.
4. Repeat until the Coder reports completion.

---

### 4. Phase 3 — Verification

Run the full quality pipeline:

```bash
make check-full
```

This executes, in order:

| Step | What it checks |
|------|---------------|
| `fmt` | `go fmt ./...` |
| `tidy` | `go mod tidy` |
| `build` | `go build` |
| `lint` | `golangci-lint run ./...` |
| `verify-architecture` | Clean/Hexagonal layer discipline (ADR checks) |
| `vulncheck` | `govulncheck` for known CVEs |
| `test` | `go test ./...` + ADR-021/022 convention checks |
| `dead-code` | Exported symbols with zero inbound references |
| `test-race` | Package-by-package race detection |
| `test-coverage` | Coverage report (excludes mocks/generated/agentinternal) |

**If any step fails:**

1. Capture the failure output.
2. `write_file` a prompt with the failure details.
3. Send to Coder: `tell-me-go -r -c ${CODER_CONFIG}` (no `--new`).
4. Retrieve response, re-run `make check-full`.
5. Repeat until all checks pass.

**If the target is a specific package** (e.g., `internal/cli/`), also verify the coverage target was met:
```bash
go test -cover ./internal/cli/...
```

---

### 5. Phase 4 — PR Creation

#### 5a. Pre-flight diff review

```bash
git diff dev -- <changed-paths>
```

Sanity-check the delta:
- Only intended files changed.
- No hardcoded secrets, no unintended production code changes.
- Test naming follows existing conventions.

#### 5b. Stage and commit

```bash
git add <changed-paths>
git commit -m "fix(<scope>): close N error handling gaps (#<issue-number>)"
```

The commit message must follow the [Git Workflow](../standards/git_workflow.md) convention.

#### 5c. Push and create PR

```bash
git push -u origin fix/issue-<NUMBER>-<short-description>
gh pr create \
  --base dev \
  --head fix/issue-<NUMBER>-<short-description> \
  --title "fix(<scope>): <description> (#<issue-number>)" \
  --body "Closes #<issue-number>.

## Summary
<brief summary of changes>

## Verification
| Check | Status |
|-------|--------|
| make check-full | PASS |
| Target coverage | ≥XX% |
"
```

---

### 6. Loop-Back Rules

| Situation | Action |
|-----------|--------|
| Architect response is vague or incomplete | Follow-up to Architect (no `--new`) asking for specifics |
| Coder hits a blocker mid-implementation | Assess: if architectural question → escalate to Architect; if clarification → answer directly |
| `make check-full` fails | Failure output → Coder (no `--new`) for fix |
| Coverage target not met | Remaining gaps → Coder (no `--new`) for additional tests |
| Coder times out or produces no output | Check `tell-me-go -t` for errors; re-send with `--new` if session corrupted |

---

### 7. Completion Checklist

- [ ] Feature branch created from `dev`
- [ ] Architect analysis retrieved and actionable
- [ ] Coder implementation completed (all gaps addressed)
- [ ] `make check-full` passes (all 10 steps)
- [ ] Target coverage met (if package-specific)
- [ ] `git diff dev` reviewed — only expected changes
- [ ] Commit message follows [Git Workflow](../standards/git_workflow.md)
- [ ] PR created targeting `dev`, linked to issue
- [ ] No uncommitted files remain (`git status` clean)
