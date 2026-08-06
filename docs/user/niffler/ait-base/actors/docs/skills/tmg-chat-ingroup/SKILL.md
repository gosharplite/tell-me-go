---
name: tmg-chat-ingroup
description: >
  Chat with other tell-me-go agents that share your workspace (the same
  session group). Use when you need to message a peer agent — architect,
  coder, reviewer, tester, or any persona defined in configs/ — instead of
  doing the work yourself. Covers discovering the roster, the TELL_ME_MODE
  golden rule (the env var that silently overrides every config's MODE),
  the stage→send→retrieve protocol, session isolation, and troubleshooting.
---

# Chatting with Agents in the Same Session Group

How to message other tell-me-go agents living in the same workspace
(`TELL_ME_HOME`) without polluting your own session or theirs.

**You might not be the butler.** You are whatever your session environment says
you are (`$TELL_ME_MODE`). This skill works for any role or persona.

## When to Activate

- You need to delegate to another agent in your group (architect, coder,
  reviewer, tester, or any persona defined in `configs/`).
- You need to know who else is in your group and how to reach them.
- You are about to run `tell-me-go` with a config that is not your own.

## The Model

- One workspace (`TELL_ME_HOME`) hosts a roster defined **entirely by the files
  in `configs/`**. Each config file = one agent = one session dir.
- The session dir comes from the **`MODE:` value inside the config**, not the
  filename. `configs/helen.yaml` → `output/helen/` because its content says
  `MODE: "helen"`.
- Config files may look very different (job-function SOPs vs. personas vs.
  different providers) — that never affects the plumbing.
- What matters: valid YAML, a valid directory-name `MODE:`, the selected
  provider defined in that config's `PROVIDERS`, and **unique `MODE:` values**
  across the group (two configs with the same MODE share one session dir and
  their histories mix).
- Session files per agent: `output/<mode>/history.jsonl` (conversation),
  `turns.log`, `tokens.log`, `tellmego.db`, `state.json`. `--new` archives
  previous session files to `output/backups/<timestamp>/`.

## 1. Find out who you can talk to

```bash
WS="$TELL_ME_HOME"
for f in "$WS/configs"/*.yaml; do
  echo "$(basename "$f") → $(yq -r '.MODE' "$f")"
done
echo "I am: $TELL_ME_MODE"
```

- **You can talk to every mode except your own.** Never message your own mode —
  that is talking to yourself (same session dir, instant self-pollution).
- To learn what an agent is/does: `yq -r '.PERSON' "$WS/configs/<file>.yaml"`
  and `yq -r '.SELECTED_PROVIDER' "$WS/configs/<file>.yaml"`.
- To see who has already run: `ls "$WS/output/"` (dirs present = agents with a
  session).

## 2. ⛔ The Golden Rule: `TELL_ME_MODE`

Your environment exports `TELL_ME_MODE=<your mode>`. tell-me-go's config loader
gives **environment variables precedence over the YAML config**, so
`TELL_ME_MODE` silently overrides the `MODE:` of *any* config you load.
Without clearing it, `-c configs/architect.yaml` still runs as **you** and
writes to **your** session dir — the reply lands in your history and the
target's `history.jsonl` stays empty.

**Always override the mode when messaging another agent:**

```bash
env -u TELL_ME_MODE TELL_ME_HOME="$WS" tell-me-go ... -c "$WS/configs/<file>.yaml"
# or explicitly:  TELL_ME_MODE=<mode> ...
```

`-c` is honored; only `MODE` is overridden by the env var. `env -u TELL_ME_MODE`
falls back to the config's own `MODE:` — the safest form.

## 3. The Three-Step Protocol

Mandatory. Inline heredocs break in `execute_command`; shared hardcoded paths
race between sessions.

**Step 1 — Stage the prompt in `/tmp` (never in `TELL_ME_HOME`):**

```bash
STAGING=$(mktemp /tmp/tell-me-go-prompt.XXXXXX)
# write_file → "$STAGING"  (use write_file, never heredocs)
test -s "$STAGING" || { echo "Prompt is empty — aborting."; exit 1; }
```

**Step 2 — Send** (fresh: `--new`; continuation: omit `--new`):

```bash
WS="$TELL_ME_HOME"
. "$WS/secrets/keys"     # exports API keys

env -u TELL_ME_MODE \
  TELL_ME_HOME="$WS" \
  tell-me-go --new -r -c "$WS/configs/<file>.yaml" < "$STAGING" &> /dev/null
```

> ⛔ Set `timeout` 600+ on every `tell-me-go` call; the default 15 s kills the
> agent mid-response.

**Provider:** by default the target runs on **your** provider — inherited from
your environment via `TELL_ME_SELECTED_PROVIDER`, so you don't set anything.
To pick a different one, add `TELL_ME_SELECTED_PROVIDER=<provider>` to the
command (see §5).

**Step 3 — Retrieve:**

```bash
env -u TELL_ME_MODE TELL_ME_HOME="$WS" \
  tell-me-go -l 1 -r -c "$WS/configs/<file>.yaml"
```

**Step 4 — Clean up:** `rm -f "$STAGING"`.

**Sanity check:** `wc -c "$WS/output/<mode>/history.jsonl"` should be non-zero,
and the run header must read `Turn N - <target mode>`. If it reads your own
mode, `TELL_ME_MODE` was not cleared.

## 4. Session Management

- `--new`: fresh session (previous files archived to `output/backups/<ts>/`).
- Omit `--new`: continuation — the target remembers prior turns.
- Reviewers: `--new` every round for independent judgment.
- Long task series (e.g. architect): `--new` once, then continue.

## 5. Provider Selection

- **Default: your own provider.** When you don't set `TELL_ME_SELECTED_PROVIDER`,
  the target inherits *your* provider from the environment — the same one you
  are running on. Nothing to specify.
- Override only when you want a different one:
  - `TELL_ME_SELECTED_PROVIDER=deepseek-pro` — cheap, same family.
  - `TELL_ME_SELECTED_PROVIDER=vertex-pro` — expensive but independent (~20×
    the per-turn cost of deepseek); use for second-opinion reviews /
    security-sensitive work.
- Any key under `PROVIDERS` in the target config is valid.

## 6. Token Monitoring

```bash
env -u TELL_ME_MODE TELL_ME_HOME="$WS" tell-me-go -t -c "$WS/configs/<file>.yaml" | tail -5
# Payload: ~62080/1000000 tokens
```

- `< 50%` — no action. `50–80%` — monitor, plan `--new` after the task.
- `> 80%` — the agent silently forgets early context; restart with `--new`.

## 7. Rules

1. Never message your own mode.
2. Always clear/override `TELL_ME_MODE` when messaging another agent.
3. Write access comes from the configs' `PERSON` text, not the plumbing —
   tell-me-go enforces no read-only. Follow whatever the configs say (in the
   conventional setup only `coder` writes; repeat the repo warning every time —
   `--new` agents have no memory).
4. `MODE:` values must be unique in the group.
5. Sequential only — never parallel `tell-me-go` calls.
6. Staging in `/tmp`, never in `TELL_ME_HOME`.
7. No heredocs — use `write_file`.
8. Generous timeout (600+).
9. Clean up staging files after use.

## 8. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Header says `Turn N - <your mode>` | `TELL_ME_MODE` not cleared | `env -u TELL_ME_MODE` |
| Target's `history.jsonl` empty (0 B) | Target never ran as its own mode | Fix `TELL_ME_MODE`, then `--new` again |
| Your history has another agent's replies | Old pollution from the same bug | Harmless; leaves with `--new` rotation |
| Two configs share a session dir | Duplicate `MODE:` values | Rename one config's `MODE:` |
| `No history found.` on retrieve | `-l` raced the send's finalize | Re-run `-l 1` |
| Provider not found | Not in that config's `PROVIDERS` | `yq eval '.PROVIDERS | keys' "$WS/configs/<file>.yaml"` |
| Call times out with no output | Default 15 s timeout | `timeout: 600` |
| `unknown shorthand flag: 'n' in -new` | Single dash parsed as `-n -e -w` | Use `--new` (double dash) |

## Quick Reference

```bash
WS="$TELL_ME_HOME"
. "$WS/secrets/keys"

# 0. Discover
for f in "$WS/configs"/*.yaml; do echo "$(basename "$f") → $(yq -r '.MODE' "$f")"; done
echo "I am: $TELL_ME_MODE"

# 1. Stage (transient — /tmp, never in $WS)
STAGING=$(mktemp /tmp/tell-me-go-prompt.XXXXXX)
#    write_file → "$STAGING"; test -s "$STAGING" || exit 1

# 2. Send (fresh) — provider defaults to your own (inherited); override with
#    TELL_ME_SELECTED_PROVIDER=<provider> only when you want a different one
env -u TELL_ME_MODE TELL_ME_HOME="$WS" \
  tell-me-go --new -r -c "$WS/configs/<file>.yaml" < "$STAGING" &> /dev/null

# 3. Retrieve
env -u TELL_ME_MODE TELL_ME_HOME="$WS" \
  tell-me-go -l 1 -r -c "$WS/configs/<file>.yaml"

# 4. Clean up
rm -f "$STAGING"
```

**Remember:** *Clear `TELL_ME_MODE`, point `-c` at the other agent's config,
and the `MODE:` inside that config picks the session dir — filename and config
content don't matter. Provider defaults to yours — no need to set it.*
