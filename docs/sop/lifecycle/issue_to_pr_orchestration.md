<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Multi-Agent Issue-to-PR Orchestration

### Objective
This SOP defines the end-to-end process for resolving a GitHub issue using the **tell-me-go** multi-agent loop (Architect + Coder), verifying the result with `make check-full`, and delivering a clean PR targeting the `dev` branch.

---

### Prerequisites

> ⛔ **CRITICAL — HARD GATE**: You **MUST** read and understand
> [`docs/steps/chatting-with-ai.md`](../../steps/chatting-with-ai.md) before
> executing this SOP. Every phase depends on the tell-me-go protocol.
> **Stop here if you cannot answer all of these:**
>
> 1. Why is `write_file` → `mktemp` + `cp` required instead of inline heredocs?
> 2. When do you use `--new` vs. omit it?
> 3. What flag retrieves the last response? What flag shows the full transcript?
> 4. What `timeout` value is **mandatory** for every `tell-me-go` call?
> 5. Why is `&> /dev/null` used on send, and how do you retrieve output afterward?
>
> If any answer is unclear, **stop** and re-read the protocol document **now**.
> This SOP cannot be executed without that knowledge.

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
| **Orchestrator** (you) | Read files, run commands, `tell-me-go` | Issue analysis, prompt writing, retrieval, verification, PR creation. Relays tasks and results between Architect and Coder. Monitors token consumption of both roles via `tell-me-go -t`. **Never modifies Go source files.** |
| **Architect** | Read-only via `tell-me-go -r` | **Phase 1:** structural analysis, risk assessment, implementation sequencing, `[ARCHITECTURAL BLOCKER]` / `[TECHNICAL DEBT]` / `[REFACTOR]` categorization. **Phase 2:** dispatches one task at a time to Coder, reviews each result, issues the next task or revision. Stays in a single session (no `--new`) across all of Phase 2. |
| **Coder** | **Write** via `tell-me-go -r` (the only role with file write access) | Implementation: receives one focused task per session (`--new` each time), TDD, table-driven tests, reports result. No memory of prior tasks — each session is self-contained. |

Communication is **asynchronous and turn-based**. The Orchestrator is the hub — Architect and Coder never talk directly.

---

## Quick Start

> ⛔ Before issuing the prompt below, confirm the orchestrator has passed the
> [Prerequisites hard gate](#prerequisites). The orchestrator **must** have read
> [`docs/steps/chatting-with-ai.md`](../../steps/chatting-with-ai.md).

**Example prompt to the AI orchestrator:**
```
Execute the Issue-to-PR SOP.
Ask me for the GitHub issue number if I haven't provided one.

Find the Architect and Coder config files. They are YAML files
co-located with this session's config directory. Use get_session_info
to locate the config_dir, then list that directory for *.yaml files.
If none are found there, fall back to the ARCHITECT_CONFIG and
CODER_CONFIG environment variables — echo them and verify the paths
exist before proceeding.

When you call ask_user for the issue number, include the resolved
config paths in the question so I can see and confirm them at a glance.
```

This prompt is **self-discovering**: it does not require the user to
hardcode config paths or the issue number. The orchestrator locates
configs from the session environment and surfaces them in the `ask_user`
prompt for easy review.

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
- A format requirement: the Architect must output **structured sections** (e.g., `## Risk Assessment`, `## Prioritized Sequence`, `## Test Strategy`, `## Pattern Guidance`). The Orchestrator will reject unstructured output.
- Phase 2 setup: *"After your analysis, you will dispatch tasks to the Coder one at a time. Tell Coder what to do with details — do not do it yourself. Output exactly one task per response, since Coder starts a new session for each task and has no prior memory. Be as clear as possible. You will review Coder's result before issuing the next task."*

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

Verify the response is actionable before proceeding. **Actionable means:**

- Has the required structured sections (`## Risk Assessment`, `## Prioritized Sequence`, `## Test Strategy`, `## Pattern Guidance`).
- Each task in the sequence references specific file paths and/or function names.
- The test strategy covers the issue's numbered acceptance criteria checklist.
- Findings are categorized (`[ARCHITECTURAL BLOCKER]`, `[TECHNICAL DEBT]`, `[REFACTOR]`).

If the response is unstructured or missing sections, send a follow-up
(**no `--new`**) asking the Architect to reformat. If the Architect asks
a question, answer via follow-up (**no `--new`**) to preserve context.

---

### 3. Phase 2 — Implementation (Architect-Driven Task Dispatch)

The Architect drives implementation **one task at a time**. This keeps
Coder sessions short (low token count, minimal hallucination) and gives
the Architect continuous visibility — no big surprises at the end.

**Session model:**

| Role | Session | Why |
|------|---------|-----|
| Architect | **Single session** (no `--new` after Phase 1) | Maintains full implementation context across all tasks |
| Coder | **New session per task** (`--new` every time) | Each task is self-contained; no context drift |

> ⛔ **Orchestrator: watch the token limit.** The Architect's single session
> grows with every task dispatch and review. Use `tell-me-go -t -c
> ${ARCHITECT_CONFIG}` to check token consumption after every 3–4
> task cycles. When the Architect's token count exceeds ~80% of the
> model's limit, the model will silently drop early messages — the
> Architect will appear to forget earlier tasks. At that point, you
> must either: (a) wrap up and proceed to Phase 3 if close to
> completion, or (b) start a new Architect session with `--new`,
> summarizing the implementation state so far.
>
> Coder sessions are naturally protected by `--new`, but verify
> periodically with `tell-me-go -t -c ${CODER_CONFIG}` to confirm
> no stale session is accumulating tokens.

**Loop:**

```
Architect outputs Task N
    → Orchestrator sends to Coder (--new)
        → Coder implements, runs tests, reports result
            → Orchestrator retrieves result, sends to Architect for review
                → Architect reviews → outputs Task N+1 or revision
                    → ... repeat until Architect declares completion
```

#### 3a. Prime the Architect for task dispatch

After retrieving the Phase 1 analysis (`-l 1`), send a follow-up prompt
to the Architect (**no `--new`** — continues the same session):

```
write_file → /tmp/tell-me-go-prompt.txt

Prompt content:
  "Begin. Output your first task."
```

The dispatch rules (one task at a time, Coder has no memory, review
each result) were already given in the Phase 1 prompt (2a). Do not
repeat them here — it wastes tokens.

Send:
```bash
PROMPT_FILE=$(mktemp) && \
cp /tmp/tell-me-go-prompt.txt "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go -r -c ${ARCHITECT_CONFIG} < "$PROMPT_FILE" &> /dev/null
```

Retrieve the Architect's first task:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG}
```

#### 3b. Send one task to Coder

The Architect's task output **is** the Coder's prompt. Save it, then
send it in a fresh session.

```bash
# Step 1: Save the Architect's task output to the prompt file
write_file → /tmp/tell-me-go-prompt.txt
    <paste the Architect's exact task output here>

# Step 2: Send to Coder (--new = fresh session, no prior memory)
PROMPT_FILE=$(mktemp) && \
cp /tmp/tell-me-go-prompt.txt "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go --new -r -c ${CODER_CONFIG} < "$PROMPT_FILE" &> /dev/null
```

Retrieve:
```bash
tell-me-go -l 1 -r -c ${CODER_CONFIG}
```

#### 3c. Send Coder's result back to Architect for review

Save the Coder's retrieved output to the prompt file, then forward to
the Architect (**no `--new`** — Architect is still in the same session):

```bash
# Step 1: Save Coder's output to the prompt file
write_file → /tmp/tell-me-go-prompt.txt
    <paste the Coder's exact output here>

# Step 2: Send to Architect for review
PROMPT_FILE=$(mktemp) && \
cp /tmp/tell-me-go-prompt.txt "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go -r -c ${ARCHITECT_CONFIG} < "$PROMPT_FILE" &> /dev/null
```

Retrieve the Architect's verdict:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG}
```

The Architect will respond with one of:
- **Next task** — the Coder's output is accepted. Save the new task to
  the prompt file and go to 3b.
- **Revision** — the Coder's output needs adjustment. The Architect's
  response **is** the corrected task. Save that text to the prompt file
  and send to Coder (`--new`) as in 3b, then re-review via 3c.
- **Completion** — all tasks done. Proceed to 3e (cross-check) before
  entering Phase 3.

#### 3d. Loop rules

Repeat 3b → 3c for each task.

| Rule | Rationale |
|------|-----------|
| Coder always gets `--new` | Each task is self-contained; no token bloat, no hallucination from stale context |
| Architect never gets `--new` during Phase 2 | Must remember all prior tasks to maintain coherent implementation |
| One task at a time | Architect reviews each result; no big surprises |
| Tasks must be self-contained | Include exact file paths, signatures, test expectations — Coder has no memory of prior tasks |

**Escape hatch:** If the same task fails 3 consecutive times (Coder
implements → Architect rejects → Coder re-implements → Architect
rejects → Coder re-implements → Architect rejects), **stop** and
escalate to the user with `ask_user`. Do not loop indefinitely.

#### 3e. Cross-check completion against the issue

When the Architect declares completion, do **not** trust it blindly.
Cross-check the issue's numbered acceptance criteria checklist against
what was implemented:

1. Read the issue body to extract the checklist (e.g., `- [ ] Fix error handling in auth.go`).
2. Run `git diff dev --stat` to see all changed files.
3. Verify each checklist item maps to at least one changed file or
   Coder-reported result.
4. If any checklist item has **no** corresponding change, send a
   follow-up to the Architect (no `--new`): *"The issue checklist
   item 'X' has no coverage. Was this addressed? If not, issue a
   task for it."*
5. Repeat until all checklist items are accounted for.

Only then proceed to Phase 3.

---

### 4. Phase 3 — Verification

> ⛔ Before entering any fix loop, check the Architect's token consumption:
> ```bash
> tell-me-go -t -c ${ARCHITECT_CONFIG}
> ```
> If >80% of the model limit, the Architect may silently drop context
> mid-fix. Restart with `--new`, summarizing the current state and the
> failure output, before issuing the first fix task.

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
3. Send to Architect: `tell-me-go -r -c ${ARCHITECT_CONFIG}` (no `--new`) for diagnosis.
4. Architect issues a fix task → send to Coder (`--new`).
5. Retrieve Coder's result → forward to Architect for review.
6. Re-run `make check-full`.
7. Repeat until all checks pass.

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
| Architect issues a task that is too large or ambiguous | Ask Architect to break it down further before forwarding to Coder |
| Coder produces incorrect or incomplete output | Send Coder's output to Architect for review; Architect will issue a revision task |
| Coder hits an error it cannot resolve | Send the error to Architect (no `--new`); Architect diagnoses and issues a corrected task |
| `make check-full` fails | Failure output → Architect (no `--new`) for diagnosis; Architect issues fix task → Coder (`--new`) |
| Coverage target not met | Remaining gaps → Architect for coverage plan → Coder (`--new`) for additional tests |
| Coder times out or produces no output | Check `tell-me-go -t -c ${CODER_CONFIG}` for errors; re-send task with fresh `--new` session |
| Same task fails 3 consecutive times | **Stop.** Escalate to user with `ask_user` — do not loop indefinitely |
| Architect appears to have forgotten context | Check `tell-me-go -t -c ${ARCHITECT_CONFIG}` for token consumption; if >80% of model limit, summarize progress and restart Architect with `--new` |
| Architect token count exceeds ~80% of model limit (proactive check) | Assess: if near completion → finish the current task then proceed to Phase 3; if mid-implementation → summarize state, restart Architect with `--new`, continue |

---

### 7. Completion Checklist

- [ ] Chat protocol complied with: no inline heredocs, `timeout: 1800` on all `tell-me-go` calls, `--new` only for first messages, retrieval via `-l N`, prompts via `write_file` → `mktemp` + `cp`
- [ ] Token limits monitored: Architect checked via `-t` during Phase 2 and before Phase 3 fix loop, Coder verified per-session, no silent context truncation
- [ ] Feature branch created from `dev`
- [ ] Architect Phase 1 analysis: structured sections present, actionable per 2c criteria
- [ ] Coder implementation completed (all gaps addressed)
- [ ] Issue checklist cross-checked (3e): every acceptance criterion maps to a change
- [ ] No task exceeded 3 consecutive retries without escalation
- [ ] `make check-full` passes (all 10 steps)
- [ ] Target coverage met (if package-specific)
- [ ] `git diff dev` reviewed — only expected changes
- [ ] Commit message follows [Git Workflow](../standards/git_workflow.md)
- [ ] PR created targeting `dev`, linked to issue
- [ ] No uncommitted files remain (`git status` clean)
