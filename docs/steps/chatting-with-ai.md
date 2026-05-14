# Chatting with Roles

How to communicate with AI roles (Architect, Coder, Tester, Reviewer) in the tell-me-go agent loop.

> **Platform**: All examples assume **Linux bash**. Not compatible with Windows PowerShell.

## Quick Reference

```bash
# Create a unique temp file for each prompt (avoids collisions)
PROMPT_FILE=$(mktemp)

# Send a prompt to a role (first message — starts a fresh session)
tell-me-go -new -r -c ${ROLE_CONFIG} < "$PROMPT_FILE" &> /dev/null

# Send a follow-up prompt (omit -new to preserve conversation history)
tell-me-go -r -c ${ROLE_CONFIG} < "$PROMPT_FILE" &> /dev/null

# Retrieve the last N responses from a role
tell-me-go -l 3 -r -c ${ROLE_CONFIG}

# View the full session transcript (for debugging)
tell-me-go -t -c ${ROLE_CONFIG}
```

> **⛔ CRITICAL — TIMEOUT**: When calling `tell-me-go` via the `execute_command` tool, you **MUST** set `timeout` to **`1800`** (1800 seconds / 30 minutes). The default 15-second hard limit will kill the sub-agent before it can complete any non-trivial task. There is no recovery — the response is lost.

## Prerequisites

Export a config file for each role you intend to use:

```bash
export ARCHITECT_CONFIG=/path/to/architect.yaml
export CODER_CONFIG=/path/to/coder.yaml
export TESTER_CONFIG=/path/to/tester.yaml
export REVIEWER_CONFIG=/path/to/reviewer.yaml
```

Each `.yaml` file defines the role's behavior, model, and system prompt.

## Flag Reference

| Flag | Purpose |
|------|---------|
| `-r` | **Role mode.** Required for all role interactions. |
| `-c <path>` | **Config file.** Path to the role's YAML configuration. |
| `-new` | **New session.** Start a fresh conversation. Omit to continue an existing one. |
| `-l <N>` | **Last N.** Retrieve the last N responses from the role (read-only). |
| `-t` | **Transcript.** Output the full execution log of the session (for debugging). |

## Sending Prompts

### Basic Pattern

Always create a unique temp file with `mktemp`, write your prompt, verify it, then send:

```bash
# 1. Create a unique temp file (prevents collisions with parallel sessions)
PROMPT_FILE=$(mktemp)

# 2. Write the prompt to the file
cat > "$PROMPT_FILE" << 'EOF'
Please review the following diff and identify architectural concerns.
EOF

# 3. Verify the file is non-empty before sending
test -s "$PROMPT_FILE" || { echo "Prompt is empty — aborting."; exit 1; }

# 4. Send to the role
tell-me-go -new -r -c ${ARCHITECT_CONFIG} < "$PROMPT_FILE" &> /dev/null
```

### Why `mktemp`?

Never use a hardcoded path like `/tmp/prompt.txt`:

| Risk | Explanation |
|------|-------------|
| **Collisions** | Two parallel tell-me-go sessions will overwrite each other's prompt. |
| **Stale data** | A crashed previous run leaves behind an old prompt — you might re-send it unknowingly. |
| **Predictability** | A fixed filename is a minor information-leak vector on multi-user systems. |

`mktemp` creates a file with a unique random suffix (e.g., `/tmp/tmp.9HzLqL`) — each invocation is guaranteed collision-free.

### Why file redirection?

- Avoids shell quoting and escaping issues that occur when piping `echo` or heredocs directly.
- Lets you inspect and verify the prompt content before sending.
- Handles multi-line content, special characters, and code blocks reliably.

### First message vs. continuation

| Scenario | Flag | Effect |
|----------|------|--------|
| Starting a brand-new task | `-new` | Resets the role's context; previous conversation is discarded. |
| Following up on the same task | *omit* `-new` | Preserves conversation history so the role remembers prior exchanges. |

Using `-new` when you meant to continue will cause the role to lose all context. Double-check before sending.

### Output suppression

`&> /dev/null` discards both stdout and stderr. This is intentional:

- Keeps **token usage low** — raw role output can be verbose and would consume your context window.
- Keeps the terminal clean while the role processes in the background.

To inspect what happened, use the retrieval commands described below.

## Retrieving Responses

### Last N responses (recommended)

```bash
tell-me-go -l 3 -r -c ${ARCHITECT_CONFIG}
```

Adjust `-l 3` to the number of responses you need. This is the primary way to read a role's output after sending a prompt.

Use `-l 1` when you only need the most recent response (the common case after a single prompt).

### Full session transcript (for debugging)

```bash
tell-me-go -t -c ${ARCHITECT_CONFIG}
```

Use this when:
- A role's response is unexpected and you need to trace the full conversation.
- You suspect the role misunderstood earlier context.
- You need to audit the complete interaction for compliance or review.

The `-t` flag does **not** require `-r`.

## Complete Workflow Example

```bash
PROMPT_FILE=$(mktemp)

# ── Step 1: Send a question to the Architect ──
cat > "$PROMPT_FILE" << 'EOF'
How should we reduce cyclomatic complexity in internal/handler/auth.go?
EOF
tell-me-go -new -r -c ${ARCHITECT_CONFIG} < "$PROMPT_FILE" &> /dev/null

# ── Step 2: Retrieve the Architect's answer ──
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG}

# ── Step 3: Ask for detailed Coder instructions (continuation — no -new) ──
cat > "$PROMPT_FILE" << 'EOF'
Provide step-by-step instructions for the Coder to implement your recommendation.
EOF
tell-me-go -r -c ${ARCHITECT_CONFIG} < "$PROMPT_FILE" &> /dev/null

# ── Step 4: Retrieve instructions and forward to Coder ──
tell-me-go -l 1 -r -c ${ARCHITECT_CONFIG} > "$PROMPT_FILE"
tell-me-go -new -r -c ${CODER_CONFIG} < "$PROMPT_FILE" &> /dev/null

# ── Step 5: Retrieve Coder's result ──
tell-me-go -l 1 -r -c ${CODER_CONFIG}
```

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| Sub-agent times out with no output | `execute_command` defaulted to 15s | Set `timeout: 1800` on every `tell-me-go` call. |
| Role seems unresponsive or returns nothing | Prompt file is empty or was not written | Run `test -s "$PROMPT_FILE"` before sending. |
| Role gives an irrelevant or confusing answer | Wrong config file (`-c` pointing to a different role) | Double-check `echo ${ROLE_CONFIG}` and the flag. |
| Can't see what the role said | Output was discarded by `&> /dev/null` | Use `tell-me-go -l N -r -c ${ROLE_CONFIG}` to retrieve it. |
| Role appears to have forgotten earlier context | `-new` was used on a follow-up message | Omit `-new` for continuations; only use it for the first message. |
| Retrieval returns stale/old responses | The `-l` count is too high and includes prior sessions | Use `-l 1` for the most recent response after each prompt. |
