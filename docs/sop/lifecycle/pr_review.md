<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Multi-Agent PR Review Orchestration

### Objective
This SOP defines the end-to-end process for reviewing a GitHub PR using two independent Architect groups (deepseek-pro + vertex-pro), resolving any gating issues via the Coder, and posting review comments from both Architects confirming the PR is ready for human merge.

---

### Prerequisites
> ⛔ **CRITICAL — HARD GATE**: You **MUST** read and understand
> [`docs/steps/chatting-with-ai.md`](../../steps/chatting-with-ai.md) before
> executing this SOP. Every phase depends on the tell-me-go protocol.
> **Stop here if you cannot answer all of these:**
>
> 1. Why is `mktemp` → `write_file` → `mktemp` + `cp` required instead of inline heredocs?
> 2. When do you use `--new` vs. omit it?
> 3. What flag retrieves the last response? What flag shows the full transcript?
> 4. What `timeout` value is **mandatory** for every `tell-me-go` call?
> 5. Why is `&> /dev/null` used on send, and how do you retrieve output afterward?
>
> If any answer is unclear, **stop** and re-read the protocol document **now**.
> This SOP cannot be executed without that knowledge.

- A GitHub PR number with an associated branch targeting `dev`.
- Two AI workspaces sourced from different providers:
  ```bash
  # Group A (deepseek-pro) — butler, architect, coder
  source /home/pos/tmp/dualnets/seed/notebooks/beta-booth/toby.sh -n vipers deepseek-pro

  # Group B (vertex-pro) — architect only
  source /home/pos/tmp/dualnets/seed/notebooks/beta-booth/toby.sh -n vipers vertex-pro
  ```
- Three role config files:
  ```bash
  export ARCHITECT_CONFIG_A="$TELL_ME_HOME/configs/architect.yaml"   # deepseek-pro
  export CODER_CONFIG_A="$TELL_ME_HOME/configs/coder.yaml"           # deepseek-pro
  export ARCHITECT_CONFIG_B="<path-to-vertex-pro-architect.yaml>"    # vertex-pro
  ```
- The `tell-me-go` binary on `PATH`.
- The `gh` CLI authenticated (`gh auth status`).
- A clean working tree: `git status` must show no uncommitted changes.

---

### Roles and Boundaries

| Role | Provider | Access | Responsibility |
|------|----------|--------|----------------|
| **Orchestrator** (you, butler) | deepseek-pro | Read files, run commands, `tell-me-go`, `gh pr` | PR fetching, prompt writing, retrieval, comment posting. Relays tasks and results between Architects and Coder. Monitors token consumption. **Never modifies Go source files.** |
| **Architect(A)** | deepseek-pro | Read-only via `tell-me-go -r` | Primary reviewer. Structural analysis, gating issue identification, fix sequencing, coder task dispatch. **Final authority**: architect(B) findings must be confirmed by architect(A) before any fix is applied. Stays in a single session across all of Phase 2. |
| **Coder(A)** | deepseek-pro | **Write** via `tell-me-go -r` (the only role with file write access) | Implementation: receives one focused fix task per session (`--new` each time), TDD, reports result. No memory of prior tasks. |
| **Architect(B)** | vertex-pro | Read-only via `tell-me-go -r` | Independent second reviewer. Provides unbiased review from a different model. Flags issues architect(A) may have missed. Does **not** dispatch tasks — findings go to architect(A) for confirmation. |

**Architect(B) findings must be confirmed by architect(A)** before any code change. If architect(A) disagrees with a finding, the Orchestrator must escalate to the human via `ask_user`. Architect(B) never instructs Coder directly.

Communication is **asynchronous and turn-based**. The Orchestrator is the hub — no role talks directly to another.

---

## Quick Start

> ⛔ Before issuing the prompt below, confirm the orchestrator has passed the
> [Prerequisites hard gate](#prerequisites). The orchestrator **must** have read
> [`docs/steps/chatting-with-ai.md`](../../steps/chatting-with-ai.md).

**Example prompt to the AI orchestrator:**
```
Execute the PR Review SOP as defined in docs/sop/lifecycle/pr_review.md.
Ask me for the GitHub PR number if I haven't provided one.

Find the Architect(A), Coder(A), and Architect(B) config files.
Try these paths, in order:

  1. This session's config directory. Use get_session_info to locate
     the config_dir, then list that directory for *.yaml files.

  2. The toby.sh convention: echo $TELL_ME_HOME. If set, check
     $TELL_ME_HOME/configs/ for architect.yaml and coder.yaml.

  3. The ARCHITECT_CONFIG_A, CODER_CONFIG_A, and ARCHITECT_CONFIG_B
     environment variables — echo them and verify the paths exist
     before proceeding.

Stop at the first path that yields all three config files.

When you call ask_user for the PR number, include the resolved
config paths in the question so I can see and confirm them at a glance.
```

---

### 0. PR Retrieval

Once you have the PR number, fetch the full PR details:

```bash
gh pr view <number> --json title,body,state,baseRefName,headRefName,additions,deletions,changedFiles
```

Also fetch the linked issue (if any) for acceptance criteria:

```bash
gh pr view <number> --json body | grep -oP '#\d+' | head -1
# If found: gh issue view <issue-number> --json title,body,state
```

Checkout the PR branch:

```bash
gh pr checkout <number>
```

Verify:
```bash
git branch --show-current
# Must be the PR's head branch
```

Save all retrieved data — you will need it for the Architect prompts.

---

### 1. Phase 1 — Architect(A) Initial Review

#### 1a. Write the Architect(A) review prompt

First, create a unique staging file:

```bash
mktemp /tmp/tell-me-go-prompt.XXXXXX
```

Then use `write_file` with that path (never inline heredocs in `execute_command`).

The prompt must include:
- The full PR body and linked issue body (if any).
- The PR diff: `gh pr diff <number>`.
- Changed file list and stats.
- A request for gating issue analysis with these categories:
  - `[GATING]` — must be fixed before human review: security vulnerabilities, data loss, broken builds, incorrect behavior, goroutine leaks, race conditions.
  - `[ADVISORY]` — should be fixed but does not block: style violations, missing tests below threshold, complexity concerns, documentation gaps.
  - `[OK]` — no issues found in the area.
- An explicit instruction to review: security, idioms (error wrapping, early returns, context propagation), concurrency (goroutine leaks, race conditions, mutex usage), architecture (package boundaries, new dependencies).
- A format requirement: the Architect must output **structured sections** (`## Gating Issues`, `## Advisory Items`, `## Overall Assessment`).
- End with: *"After your review, you will dispatch fix tasks to the Coder one at a time if gating issues exist. Output exactly one task per response. Coder starts a new session for each task and has no prior memory. Be as clear as possible with exact file paths and function signatures."*

#### 1b. Send to Architect(A)

```bash
STAGING=/tmp/tell-me-go-prompt.XXXXXX && \
PROMPT_FILE=$(mktemp) && \
cp "$STAGING" "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go --new -r -c ${ARCHITECT_CONFIG_A} < "$PROMPT_FILE" &> /dev/null
```

> **CRITICAL**: Set `timeout: 1800` on every `tell-me-go` call. The default 15s limit will kill the sub-agent.

#### 1c. Retrieve Architect(A) response

```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_A}
```

**Verify the response is actionable:**
- Has the required structured sections (`## Gating Issues`, `## Advisory Items`, `## Overall Assessment`).
- Each gating issue references specific file paths and/or line numbers.
- The overall assessment clearly states whether gating issues exist.

If the response is unstructured or missing sections, send a follow-up
(**no `--new`**) asking Architect(A) to reformat.

#### 1d. Determine next action

| Architect(A) finding | Action |
|----------------------|--------|
| **No gating issues** | Skip to Phase 2 — post Architect(A) comment on PR |
| **Gating issues exist** | Continue to 1e |

#### 1e. Fix gating issues (Architect(A) → Coder(A) loop)

The Architect(A) drives fixes **one task at a time**. Same session model as
`issue_to_pr_orchestration.md`:

| Role | Session | Why |
|------|---------|-----|
| Architect(A) | **Single session** (no `--new` after Phase 1) | Maintains full fix context across all tasks |
| Coder(A) | **New session per task** (`--new` every time) | Each task is self-contained; no context drift |

> ⛔ **Orchestrator: watch the token limit.** Use `tell-me-go -t -c
> ${ARCHITECT_CONFIG_A} | tail -5` after every 3–4 task cycles. When
> >80% of model limit, restart Architect(A) with `--new`, summarizing
> the current fix state.

**Loop:**

```
Architect(A) outputs Task N
    → Orchestrator sends to Coder(A) (--new)
        → Coder(A) implements, runs tests, reports result
            → Orchestrator retrieves, sends to Architect(A) for review
                → Architect(A) reviews → Task N+1, REVISION, or DONE
                    → ... repeat until Architect(A) declares DONE
```

##### 1e-i. Prime Architect(A) for task dispatch

Send a follow-up (**no `--new`**):

First, create a unique staging file (use `mktemp`).

Prompt content:
```
"Begin. Output your first fix task.
 Prefix every response with exactly one of:
   TASK: <description>    — a new task for Coder
   REVISION: <correction> — Coder's last output needs adjustment
   DONE.                  — all gating issues resolved
 The prefix must be the first word(s) of your response."
```

Send:
```bash
STAGING=/tmp/tell-me-go-prompt.XXXXXX && \
PROMPT_FILE=$(mktemp) && \
cp "$STAGING" "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go -r -c ${ARCHITECT_CONFIG_A} < "$PROMPT_FILE" &> /dev/null
```

Retrieve:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_A}
```

##### 1e-ii. Send one task to Coder(A)

Save Architect(A)'s task output, send in fresh session (`--new`):

```bash
STAGING=/tmp/tell-me-go-prompt.XXXXXX && \
PROMPT_FILE=$(mktemp) && \
cp "$STAGING" "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go --new -r -c ${CODER_CONFIG_A} < "$PROMPT_FILE" &> /dev/null
```

Retrieve:
```bash
tell-me-go -l 1 -r -c ${CODER_CONFIG_A}
```

##### 1e-iii. Send Coder(A) result back to Architect(A)

Save Coder(A)'s output, forward to Architect(A) (**no `--new`**):

```bash
STAGING=/tmp/tell-me-go-prompt.XXXXXX && \
PROMPT_FILE=$(mktemp) && \
cp "$STAGING" "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go -r -c ${ARCHITECT_CONFIG_A} < "$PROMPT_FILE" &> /dev/null
```

Retrieve:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_A}
```

Architect(A) responds with `TASK:`, `REVISION:`, or `DONE.`

##### 1e-iv. Fix loop rules

| Rule | Rationale |
|------|-----------|
| Coder always `--new` | Self-contained tasks; no token bloat |
| Architect(A) never `--new` during fixes | Must remember all prior fixes |
| One task at a time | Architect reviews each result |
| Same task fails 3x → escalate | Use `ask_user`; do not loop indefinitely |
| Architect(A) token >80% → restart | Summarize state, restart with `--new` |

When Architect(A) declares `DONE.`, proceed to Phase 2.

---

### 2. Phase 2 — Architect(A) Post-Review Comment

After gating issues are resolved (or if none were found), Architect(A) posts a
final review comment on the PR.

#### 2a. Generate the comment

Create a staging file with the prompt:

```
"You have completed your review of PR #<number>.
 Write a concise review comment summarizing:
 - What you reviewed (files, areas).
 - Your findings (gating issues found and resolved, or no gating issues).
 - Your final assessment: the PR has no remaining gating issues and is ready for Architect(B) review.

 Format as a GitHub-style comment suitable for 'gh pr comment'."
```

Send to Architect(A) (**no `--new`** — continues existing session):

```bash
tell-me-go -r -c ${ARCHITECT_CONFIG_A} < "$PROMPT_FILE" &> /dev/null
```

Retrieve:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_A}
```

#### 2b. Post the comment

```bash
gh pr comment <number> --body "<Architect(A)'s comment>"
```

Verify:
```bash
gh pr view <number> --json comments | head -20
```

---

### 3. Phase 3 — Architect(B) Independent Review

Architect(B) (vertex-pro) provides a second, independent review. Architect(B)
has no knowledge of Architect(A)'s findings.

#### 3a. Write the Architect(B) review prompt

Use `mktemp` → `write_file` pattern.

The prompt must include:
- The full PR body and linked issue body (if any).
- The PR diff: `gh pr diff <number>`.
- Changed file list and stats.
- The same review criteria as Phase 1 (security, idioms, concurrency, architecture).
- A format requirement: `## Gating Issues`, `## Advisory Items`, `## Overall Assessment`.
- Architect(B) does **not** dispatch tasks — only reports findings.

#### 3b. Send to Architect(B)

```bash
STAGING=/tmp/tell-me-go-prompt.XXXXXX && \
PROMPT_FILE=$(mktemp) && \
cp "$STAGING" "$PROMPT_FILE" && \
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; } && \
tell-me-go --new -r -c ${ARCHITECT_CONFIG_B} < "$PROMPT_FILE" &> /dev/null
```

Retrieve:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_B}
```

#### 3c. Determine next action

| Architect(B) finding | Action |
|----------------------|--------|
| **No gating issues** | Skip to Phase 4 — post Architect(B) comment |
| **Gating issues exist** | Continue to 3d |

#### 3d. Confirm gating issues with Architect(A)

> ⛔ **Architect(A) is the final authority.** Architect(B) findings must be
> confirmed by Architect(A) before any code change.

Send Architect(B)'s gating issue findings to Architect(A) (**no `--new`**):

```
"Architect(B) (vertex-pro) found the following gating issues in
 PR #<number>:

 <paste Architect(B)'s gating issues here>

 For each issue, respond with one of:
   CONFIRMED: <issue> — I agree this is a gating issue
   DISPUTED: <issue> — <reasoning> — I disagree this is gating

 Only CONFIRMED issues will be fixed."
```

Retrieve Architect(A)'s response:
```bash
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_A}
```

| Architect(A) response | Action |
|-----------------------|--------|
| All CONFIRMED | Proceed to fix loop (3e) |
| Any DISPUTED | **Escalate to human** via `ask_user` with both Architects' reasoning |
| Mixed | Fix CONFIRMED items; escalate DISPUTED items |

#### 3e. Fix confirmed issues

Same loop as 1e (Architect(A) dispatches → Coder(A) implements → Architect(A) reviews).

After fixes, re-run Architect(B) review to confirm resolution:

```bash
tell-me-go --new -r -c ${ARCHITECT_CONFIG_B} < "$PROMPT_FILE" &> /dev/null
```

Repeat 3c → 3d → 3e until both Architects agree no gating issues remain.

---

### 4. Phase 4 — Architect(B) Post-Review Comment

Generate and post Architect(B)'s final comment, same pattern as Phase 2:

```bash
# Generate comment via Architect(B) (--new session)
tell-me-go --new -r -c ${ARCHITECT_CONFIG_B} < "$PROMPT_FILE" &> /dev/null
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG_B}

# Post
gh pr comment <number> --body "<Architect(B)'s comment>"
```

---

### 5. Phase 5 — Final State Verification

Both Architects have posted comments. Verify the PR state:

```bash
gh pr view <number> --json comments,state
```

**Final state:**
- Architect(A) comment present — confirms no gating issues.
- Architect(B) comment present — confirms no gating issues.
- PR remains **open** — human reviews and merges.
- No `--approve` is posted. The AI does not approve PRs.

---

### 6. Loop-Back Rules

| Situation | Action |
|-----------|--------|
| Architect(A) response vague or incomplete | Follow-up (no `--new`) asking for specifics |
| Architect(A) issues task too large | Ask Architect(A) to break it down before forwarding to Coder |
| Coder produces incorrect output | Send to Architect(A) for review; Architect issues revision |
| Coder hits unresolvable error | Send error to Architect(A) (no `--new`) for diagnosis |
| Same task fails 3 consecutive times | **Stop.** Escalate to user with `ask_user` |
| Architect(A) token >80% | Summarize state, restart Architect(A) with `--new` |
| Architect(B) finds gating issue, Architect(A) disputes | **Stop.** Escalate to user with both reasonings |
| Architect(B) times out or errors | Retry once with fresh `--new`; if still fails, proceed with Architect(A) only and note in comments |
| Coder times out | Check `tell-me-go -t -c ${CODER_CONFIG_A} \| tail -5`; re-send with fresh `--new` |

---

### 7. Completion Checklist

- [ ] Chat protocol complied with: no inline heredocs, `timeout: 1800` on all `tell-me-go` calls, `--new` only for first/new messages, retrieval via `-l N`, prompts via `mktemp` → `write_file` → `mktemp` + `cp`
- [ ] Token limits monitored: Architect(A) checked during fix loop, Coder(A) verified per-session
- [ ] PR branch checked out and verified
- [ ] Architect(A) initial review: structured sections present
- [ ] All gating issues from Architect(A) resolved (or none found)
- [ ] Architect(A) comment posted on PR
- [ ] Architect(B) independent review completed
- [ ] All gating issues from Architect(B) confirmed by Architect(A)
- [ ] Confirmed issues resolved via Coder(A)
- [ ] Architect(B) re-review confirms resolution
- [ ] Architect(B) comment posted on PR
- [ ] Both Architects agree: no gating issues remain
- [ ] PR remains **open** — no auto-approve
- [ ] No uncommitted changes remain (`git status` clean)
- [ ] No task exceeded 3 consecutive retries without escalation
