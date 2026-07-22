# Evolution of Environment Management

How `tell-me-go` environment management evolved from simple shell aliases to a multi-role, provider-switchable system — all in Bash.

---

## Stage 1: Bash Aliases

The starting point. Users add a handful of shell aliases and a single environment variable to their `.bashrc` or `.zshrc`:

```bash
# 1. Set the home directory for session history and global state
export TELL_ME_HOME="$HOME/tell-me-go"

# 2. Main command alias (b for tell-me-go)
b() {
    tell-me-go -c "$TELL_ME_HOME/configs/assistant.yaml" "$@"
}
export -f b

# 3. Quick session undo
alias bb='b -b'
# 4. Show last 10 messages
alias bl='b -l 10'
# 5. Start fresh session
alias b-new='b --new'
# 6. Maintenance: Update and install
alias b-install='(cd $TELL_ME_HOME && git pull && go install ./cmd/tell-me-go)'
```

### What It Gives You

| Feature | How |
|---------|-----|
| **One config** | A single `assistant.yaml` at `$TELL_ME_HOME/configs/` |
| **One provider** | Hard-coded `SELECTED_PROVIDER` in that YAML |
| **One session** | All history, tasks, and `SafePath` live under `$TELL_ME_HOME/output/` |
| **Short commands** | `b`, `bb`, `bl`, `b-new`, `b-install` |

### Limitations

- **Single provider**: Switching LLMs means manually editing `assistant.yaml`.
- **Single role**: Only `assistant.yaml` — no support for coder, reviewer, architect personas.
- **No isolation**: All projects share the same session history and state.
- **No interactive selection**: Providers and environments are purely file-based.

---

## Stage 2: Toby — Tag-Based Environment Manager

Toby addresses the single-provider, single-role, and no-isolation limitations by introducing **tag-based environments**. Each project gets its own `ait-<tag>-<provider>` directory cloned from a shared template, with the provider baked into the directory name.

### How It's Sourced

```bash
# In .bashrc
source /path/to/toby.sh

# Or interactively:
source toby.sh           # fzf picker for existing environments
source toby.sh -n         # fzf picker, create if tag is new
source toby.sh -n myproject vertex-flash  # direct
```

### What It Creates

Each environment is a self-contained copy of the `ait-base` template:

```text
ait-<tag>-<provider>/
├── configs/
│   ├── butler.yaml       # Master config (all providers + pricing)
│   ├── architect.yaml    # Derived from butler, architect persona
│   ├── coder.yaml        # Derived from butler, coder persona
│   ├── reviewer.yaml     # Derived from butler, reviewer persona
│   └── tester.yaml       # Derived from butler, tester persona
├── secrets/
│   ├── key.json          # Service account credentials
│   └── keys              # API key env vars (sourced)
├── docs/skills/          # Shared skill/pattern files
└── output/               # Isolated session data per environment
```

### Key Features

| Feature | How |
|---------|-----|
| **Tag-based isolation** | Each `ait-<tag>-<provider>` has its own configs, secrets, history, tasks, and `SafePath` |
| **Interactive selection** | `fzf`-based picker for tags and providers, with numbered-menu fallback |
| **Role configs** | Five role configs (butler, architect, coder, reviewer, tester) all derived from a single `butler.yaml` master — each preserves its own `PERSON` persona |
| **API key retargeting** | On creation, copies keys from the shared template and rewrites YAML paths to point to the local environment's copy |
| **Auto context window** | `MAX_HISTORY_TOKENS` is automatically set from the selected model's `CONTEXT_WINDOW` |
| **Priority mode** (`-p`) | Uses `ait-base-priority` template with Vertex AI shared/priority headers for higher-throughput quota pools |
| **Clean mode** (`-c`) | Skips copying the `docs/` folder for lean environments |
| **Visual prompt** | `PS1` prepended with `[tag|provider]` in color |
| **Single-source guard** | Detects if already sourced and refuses to double-source, preventing accidental state overwrite |

### Helper Functions

```bash
b "Hello!"   # Butler — general assistant
c "Write..." # Coder — implementation
t "Test..."  # Tester — QA
r "Review..."# Reviewer — code review
a "Design..."# Architect — design review
```

Each reads from `$TELL_ME_HOME/configs/<role>.yaml` with the active provider and tag baked into `MODE` and `SELECTED_PROVIDER`.

### What It Solves vs. Stage 1

| Limitation | Stage 1 | Stage 2 (Toby) |
|------------|---------|-----------------|
| Single provider | Manual YAML edit | `source toby.sh <tag> <provider>` |
| Single role | `assistant.yaml` only | Five role configs (b/a/c/r/t) |
| No isolation | Shared `$TELL_ME_HOME` | Per-environment `ait-<tag>-<provider>/` |
| No interactive picker | File-based only | `fzf` with fallback |

### Remaining Limitations

- **Provider baked into directory name**: Switching providers for the same project means a new `ait-<tag>-<new-provider>` directory — losing session history, tasks, and `SafePath` from the old one.
- **No mid-session switching**: The provider is fixed at source time. Comparing models on the same prompt requires two separate environments.
- **Single master config**: `butler.yaml` is the source of truth; adding a new provider means updating the template.

---

## Stage 3: Dobby — Multi-Role with Mid-Session Provider Switching

Dobby's key innovation: the provider is **not** baked into the directory name. Environments are named `ait-<tag>` (no `-<provider>` suffix), so switching providers keeps the same session history, tasks, and `SafePath`. You can even hot-swap providers mid-session without opening a new terminal.

### How It's Sourced

```bash
# In .bashrc
source /path/to/dobby.sh -n myproject vertex-flash

# Interactively:
source dobby.sh                      # fzf picker for tag + provider
source dobby.sh -n                   # fzf picker, create if tag is new
source dobby.sh -n -p                # create with Vertex priority headers
source dobby.sh -n -c                # create without docs/ folder
source dobby.sh myproject vertex-flash  # direct

# Mid-session provider switch (same tag, same terminal):
source dobby.sh openai               # hot-swap LLM, keep all state
```

### Directory Structure

```text
ait-<tag>/                           # Provider NOT in the name
├── .gitignore                       # Ignores output/ and secrets/
├── configs/
│   ├── butler.yaml                  # Master config (all providers + pricing)
│   ├── butler-priority.yaml         # Variant with Vertex priority headers
│   ├── architect.yaml               # Generated from butler, architect persona
│   ├── coder.yaml                   # Generated from butler, coder persona
│   ├── reviewer.yaml                # Generated from butler, reviewer persona
│   └── tester.yaml                  # Generated from butler, tester persona
├── secrets/
│   ├── key.json                     # Service account credentials
│   └── keys                         # API key env vars (sourced)
├── docs/skills/                     # Shared skill/pattern files
└── output/                          # Isolated session data
```

### Key Features

| Feature | How |
|---------|-----|
| **Provider not in path** | `ait-<tag>` instead of `ait-<tag>-<provider>` — provider is selected at source time, not encoded in the directory |
| **Mid-session switching** | `source dobby.sh <new-provider>` hot-swaps the LLM while keeping the same tag, session history, tasks, and `SafePath` |
| **Template-based provisioning** | `-n` clones `ait-base/`, regenerates all role configs from `butler.yaml` while preserving each role's `PERSON` |
| **Priority mode** (`-p`) | Generates role configs from `butler-priority.yaml` with Vertex AI shared/priority headers |
| **Clean mode** (`-c`) | Skips copying the `docs/` folder |
| **Re-entry guard** | Detects if already sourced — if so, allows **provider-only** switching (rejects `-n` since the tag is fixed) |
| **Prompt preservation** | Saves original `PS1` on first source, restores and re-applies on re-entry so prefixes never stack |
| **Role functions** | `b` (butler), `a` (architect), `c` (coder), `t` (tester), `r` (reviewer) — each sets `TELL_ME_MODE`, `TELL_ME_SELECTED_PROVIDER`, and `TELL_ME_HOME` at invocation time |

### Mid-Session Provider Switching in Detail

This is Dobby's standout feature. Since the provider is not in the directory name:

```bash
# Start with Vertex Flash
source dobby.sh myproject vertex-flash
b "Write a regex for email validation"
# Session history, tasks, SafePath all live in ait-myproject/output/

# Switch to OpenAI — same tag, same terminal, same state
source dobby.sh openai
b "Write a regex for email validation"
# Compares models on the same prompt, with full session context preserved
```

The re-entry guard detects `DOBBY_SOURCED`, strips the old prompt prefix, and re-applies with the new provider. The tag stays fixed; only the provider changes.

### What It Solves vs. Stage 2

| Limitation (Toby) | Stage 3 (Dobby) |
|--------------------|------------------|
| Provider baked into directory name | `ait-<tag>` — provider is a runtime choice |
| Switching provider = new directory, lost state | Mid-session `source dobby.sh <provider>` keeps all state |
| Provider comparison needs two environments | Same prompt, same tag, hot-swap and re-run |

---

## Stage 4: Porter & Sprawl — Self-Contained Workspaces and Sub-Agent Orchestration

Stage 4 shifts from *human* environment management to enabling the **butler AI itself** to spawn and orchestrate sub-agents. Two tools emerge: **Porter** (human-facing, self-contained workspace stager) and **Sprawl** (headless sub-agent spawner for the butler to use inside pipeline pods).

### Porter — Self-Contained Workspace Stager

Porter is a single Bash script with everything embedded — configs, secrets, skills, role personas. No external template directory. Copy it anywhere and source it.

```bash
source porter              # fzf pick provider, tag = random hex
source porter deepseek-pro # explicit provider
source porter -p           # Vertex priority tier headers
source porter -c           # skip docs/skills

b --new "hello"            # talk to the butler
db vertex-pro              # switch provider mid-session (via dobby.sh)
```

**What makes it different from Dobby:**

| | Dobby | Porter |
|---|---|---|
| **Template** | External `ait-base/` directory | Fully embedded in the script |
| **Workspace** | Named `ait-<tag>/` in a fixed base dir | `/tmp/dobby.XXXXXX/` (random, ephemeral) |
| **Deployment** | Requires the full `dobby/` directory tree | Single file, copy anywhere |
| **Secrets** | Copied from template on provision | Embedded (API keys, service account JSON) |
| **Role configs** | Generated from `butler.yaml` at provision time | Generated on-the-fly from embedded YAML |
| **Tag** | Human-chosen name (`myproject`) | Random hex (`aBcDeF`) |

Porter integrates with Dobby: after sourcing porter once, the `db` alias (dobby.sh) can still switch providers within the same ephemeral workspace.

### Sprawl — Sub-Agent Spawner

Sprawl (`sprawl-agent.sh`) is designed for the **butler AI** to spawn isolated sub-agents inside pipeline pods. It creates a disposable `/tmp/dobby.XXXXXX/` workspace pre-configured for a specific role and provider.

```bash
eval $(bash sprawl-agent.sh architect deepseek-pro)
# → exports: ROLE, PROVIDER, DOBBY_HOME, CONFIG, TMP

# Send prompt (fresh session):
tell-me-go --new -r -c "$CONFIG" < prompt_file

# Follow-up (same session):
tell-me-go -r -c "$CONFIG" < prompt_file

# Retrieve last response:
tell-me-go -l 1 -r -c "$CONFIG"
```

**Key differences from Porter:**

| | Porter | Sprawl |
|---|---|---|
| **Who runs it** | Human in a terminal | Butler AI via `execute_command` |
| **Purpose** | Interactive env with `b`/`a`/`c`/`t`/`r` | Headless, single-role sub-agent |
| **Shell functions** | Yes | No — raw `tell-me-go` calls |
| **Session model** | Multi-turn interactive | Single-task, then discard |
| **Provider switching** | Via `db` alias | N/A — one provider per spawn |

### The Three-Step Protocol

Communication with sub-agents follows a mandatory three-step protocol to avoid heredoc breakage, path collisions, and race conditions:

**Step 1 — Create a unique staging file:**
```bash
mktemp /tmp/tell-me-go-prompt.XXXXXX
# → /tmp/tell-me-go-prompt.9HzLqL
```

**Step 2 — Write the prompt** using the `write_file` tool (never inline heredocs in `execute_command`).

**Step 3 — Send to sub-agent:**
```bash
PROMPT_FILE=$(mktemp) && cp "$STAGING" "$PROMPT_FILE" && \
TELL_ME_HOME="$DOBBY_HOME" tell-me-go --new -r -c "$CONFIG" < "$PROMPT_FILE"
```

### Multi-Agent Orchestration Pattern

The butler spawns independent sub-agents and routes work through them:

```
Butler
  ├── spawns Architect (deepseek-pro)  → analyzes issue, plans implementation
  ├── spawns Coder (deepseek-pro)      → implements each task (--new per task)
  ├── spawns Reviewer (deepseek-pro)   → reviews code against idioms/security
  └── spawns Tester (deepseek-pro)     → verifies coverage, flags edge cases
```

**Session strategy:**
- **Architect**: single session across all tasks (preserves context for consistent planning)
- **Coder**: `--new` per task (each implementation is self-contained)
- **Reviewer/Tester**: `--new` per review cycle

**Token monitoring**: Sub-agents are checked periodically — if token usage exceeds ~80% of context window, the butler restarts with `--new` to prevent silent context truncation.

### Persisting State Across Subshells

Each `execute_command` is a fresh subshell — spawn variables from `eval` don't persist. The butler writes them to a tracking file:

```bash
# After spawning all agents, write paths to a file:
. /tmp/sprawl-paths.env

# In subsequent commands:
. /tmp/sprawl-paths.env && \
  TELL_ME_HOME="$ARCHITECT_HOME" tell-me-go --new -r -c "$ARCHITECT_CONFIG" < prompt
```

Communication is strictly **sequential** — parallel sub-agent calls are explicitly avoided due to subshell variable inheritance issues.

### What It Solves vs. Stage 3

| Limitation (Dobby) | Stage 4 (Porter & Sprawl) |
|--------------------|---------------------------|
| Requires external template directory | Porter: single self-contained script; Sprawl: copies from parent session |
| Human-only operation | Butler AI can spawn and orchestrate sub-agents |
| Fixed workspace location | Ephemeral `/tmp/dobby.XXXXXX/` — disposable, collision-free |
| One role at a time | Multiple concurrent sub-agents, each with isolated state |
| Manual role handoff | Automated Architect → Coder → Reviewer loop |

---

## Stage 5: SSH Sub-Agent Sprawl — Remote Orchestration

Stage 5 extends sub-agent orchestration **across machines**. The butler now spawns sub-agents on remote hosts via SSH, with prompt delivery through stdin pipes rather than local files. This offloads LLM work to dedicated compute and enables session survival across local reboots.

### Architecture

```
┌──────────────────────┐         SSH          ┌──────────────────────┐
│    Orchestrator      │ ─── stdin pipe ───→  │    Remote Host        │
│    (local)           │                      │                       │
│                      │ ←── stdout    ────── │  /tmp/porter          │
│  ais-ssh/            │                      │  /tmp/dobby.XXXXXX/   │
│  ssh-sprawl-agent.sh │                      │  ├── configs/         │
│  /tmp/ssh-sprawl-    │                      │  ├── secrets/         │
│    paths.env         │                      │  └── output/          │
└──────────────────────┘                      └──────────────────────┘
```

### Spawning a Remote Sub-Agent

```bash
eval $(bash ssh-sprawl-agent.sh <host> architect deepseek-pro)
# → exports: SSH_HOST, ROLE, PROVIDER, DOBBY_HOME, CONFIG, TMP
```

What happens under the hood:

1. SSH's into the remote host
2. Sources `/tmp/porter` (pre-deployed) with the given provider — porter creates the full workspace with all role configs, secrets, and skills
3. Captures `DOBBY_HOME`, `DOBBY_TAG`, etc. from the remote shell
4. Writes paths to both stdout (for `eval`) and `/tmp/ssh-sprawl-paths.env` (for cross-subshell persistence)

### The Tracking File

Each sub-agent gets a role-keyed prefix in `/tmp/ssh-sprawl-paths.env`:

| Role | Key prefix | Example variables |
|------|-----------|-------------------|
| butler | `B_` | `B_HOST`, `B_HOME`, `B_CONFIG`, `B_PROVIDER` |
| architect | `A_` | `A_HOST`, `A_HOME`, `A_CONFIG`, `A_PROVIDER` |
| coder | `C_` | `C_HOST`, `C_HOME`, `C_CONFIG`, `C_PROVIDER` |
| tester | `T_` | `T_HOST`, `T_HOME`, `T_CONFIG`, `T_PROVIDER` |
| reviewer | `R_` | `R_HOST`, `R_HOME`, `R_CONFIG`, `R_PROVIDER` |

Keys include sanitized host and provider names (e.g., `A_websc_34_80_110_236_deepseek_pro`), allowing one agent per role per provider per host with no collisions.

### The SSH Three-Step Protocol

Every remote sub-agent interaction follows the same pattern as local sprawl, but with SSH stdin pipes instead of local file descriptors:

**Step 1 — Create a unique staging file (local):**
```bash
mktemp /tmp/tell-me-go-prompt.XXXXXX
```

**Step 2 — Write the prompt** using the `write_file` tool.

**Step 3 — Send via SSH stdin pipe (fresh session):**
```bash
. /tmp/ssh-sprawl-paths.env

ssh ${A_HOST} -- "source ${A_HOME}/secrets/keys; \
  TELL_ME_SELECTED_PROVIDER=${A_PROVIDER} TELL_ME_HOME=${A_HOME} \
  tell-me-go --new -r -c ${A_CONFIG}" < "$PROMPT_FILE"
```

**Step 4 — Retrieve response:**
```bash
. /tmp/ssh-sprawl-paths.env

ssh ${A_HOST} -- "TELL_ME_SELECTED_PROVIDER=${A_PROVIDER} TELL_ME_HOME=${A_HOME} \
  tell-me-go -l 1 -r -c ${A_CONFIG}"
```

### Why SSH stdin pipe (not scp)?

| Approach | Problem |
|----------|---------|
| `scp` prompt file to remote | Extra round-trip, stale file risk, cleanup burden |
| Heredocs in `execute_command` | Shell escaping breaks multi-line prompts |
| **SSH stdin pipe** (`< file`) | Prompt never touches remote disk, no cleanup needed |

The prompt is streamed directly into `tell-me-go`'s stdin via SSH. No temp files on the remote side.

### Secrets: Must Source Before Every Call

Unlike local sub-agents where env vars persist in the shell, each SSH command is a **fresh shell**. API keys must be sourced every time:

```bash
source ${A_HOME}/secrets/keys
```

This is embedded before every `tell-me-go` invocation. Without it, env-var-based providers (DeepSeek, OpenAI, Anthropic) fail with authentication errors. No secrets are transmitted over SSH — they live on the remote host.

### Key Differences: Local vs SSH

| | Stage 4 (Local Sprawl) | Stage 5 (SSH Sprawl) |
|---|---|---|
| **Spawning** | `eval $(bash sprawl-agent.sh ...)` | `eval $(bash ssh-sprawl-agent.sh <host> ...)` |
| **Workspace location** | Local `/tmp/dobby.XXXXXX/` | Remote `/tmp/dobby.XXXXXX/` |
| **Prompt delivery** | `tell-me-go < file` (local FD) | `ssh ... "tell-me-go ..." < file` (stdin pipe) |
| **Response retrieval** | `tell-me-go -l 1 -r -c "$CONFIG"` | `ssh ... "tell-me-go -l 1 -r -c \$CONFIG"` |
| **API keys** | Inherited from parent env | Sourced from `$DOBBY_HOME/secrets/keys` each call |
| **Session persistence** | Lost on local reboot | Survives local reboot (remote persists) |
| **`porter` dependency** | None (uses existing `TELL_ME_HOME`) | Must be deployed on remote (`/tmp/porter`) |

### Prerequisites (One-Time Per Host)

1. **Deploy porter**: `scp porter <host>:/tmp/porter`
2. **Install tools**: `tell-me-go`, `yq`, `fzf` on the remote (porter auto-discovers them at `/usr/local/bin`, `/usr/local/go/bin`, and `$HOME/go/bin`)
3. **Verify**: A single `ls` command checks porter + all tools before spawning

### Session Strategy

Same as local sprawl, now remote-aware:
- **Architect**: single session across all tasks (context preserved on remote)
- **Coder**: `--new` per task
- **Reviewer/Tester**: `--new` per review cycle
- **Communication is strictly sequential** — concurrent writes to the same remote SQLite DB will corrupt the session

### Cleanup (Mandatory)

Three-step cleanup at end of session, **in order**:

1. **Remove workspaces**: Iterate `/tmp/ssh-sprawl-paths.env`, `ssh <host> -- "rm -rf <tmp>"` for each remote workspace
2. **Remove porter**: `ssh <host> -- "rm -rf /tmp/porter"` from every remote host — porter embeds secrets, leaving it behind is a security risk
3. **Remove local artifacts**: `rm -f /tmp/ssh-sprawl-paths.env /tmp/tell-me-go-prompt.*`

Step ordering matters: workspaces → porter → local artifacts. The tracking file must survive partial failures so cleanup can be retried.

### What It Solves vs. Stage 4

| Limitation (Stage 4) | Stage 5 (SSH Sprawl) |
|-----------------------|----------------------|
| All agents run locally — compete for CPU/memory | Agents offloaded to dedicated remote hosts |
| Session lost on local reboot | Remote sessions persist independently |
| Single-machine bottleneck | Horizontally scalable across hosts |
| No cross-machine orchestration | Butler orchestrates agents on any reachable host |

---

## Stage 6: Winky — Agent-Deployed Remote Dev Environment

Stage 5 (SSH Sprawl) is designed for the **butler AI** to orchestrate sub-agents. But what about a **human** who wants a quick interactive `tell-me-go` environment on a remote host — without manually copying templates, installing tools, and configuring secrets?

Winky bridges this gap. The butler AI deploys the infrastructure (tools, template, script) to a remote host, then **stops**. The human SSH's in and sources `winky.sh` to pick their own tag and provider.

### The Agent/User Boundary

This is Winky's defining design principle — a hard boundary between what the agent does and what the user does:

| Steps | Who | What |
|-------|-----|------|
| **0–5** | **Agent (butler)** | Check host, install tools, create `/tmp/winky/`, deploy `winky.sh` and `ait-base/` |
| **6** | **User** | SSH in and `source winky.sh -n` to pick their own tag |

The agent **must not** source `winky.sh` or create any `ait-<tag>/` directory. The tag (`coder`, `butler`, `tester`, etc.) is the user's choice.

### Agent Steps (0–5)

**Step 0 — Confirm target host.** Ask the user which host.

**Step 1 — Check tools:**
```bash
ssh <host> -- 'for tool in fzf yq tell-me-go; do
  ls /usr/local/bin/$tool /usr/local/go/bin/$tool $HOME/go/bin/$tool 2>/dev/null | head -1 || echo "$tool: MISSING"
done'
```

**Step 2 — Install missing binaries.** Same toolchain as SSH Sprawl (tell-me-go, yq, fzf).

**Step 3 — Create workspace:**
```bash
ssh <host> -- 'mkdir -p /tmp/winky'
```

**Step 4 — Deploy winky.sh:**
```bash
scp winky.sh <host>:/tmp/winky/winky.sh
```

**Step 5 — Deploy ait-base template:**
```bash
scp -r ait-base <host>:/tmp/winky/ait-base
```

After Step 5, the remote contains only:
```
/tmp/winky/
  winky.sh
  ait-base/
    configs/
    docs/
    output/
    secrets/
```

No `ait-<tag>/` directory exists — the agent stopped at the right time. If one does, the agent overstepped.

### User Step (6)

```bash
ssh <host>
source /tmp/winky/winky.sh -n
```

`winky.sh` uses `fzf` to let the user pick a **tag** and **provider**, then creates `ait-<tag>/` from the `ait-base/` template. After creation, it functions identically to Dobby:

```bash
b "Hello!"   # Butler
a "Design..."# Architect
c "Write..." # Coder
t "Test..."  # Tester
r "Review..."# Reviewer

# Mid-session provider switch:
source /tmp/winky/winky.sh openai
```

The prompt updates to `[tag|provider]` and all helper functions are available.

### What Winky Is (and Isn't)

Winky is essentially **Dobby deployed to a remote host at `/tmp/winky/`**. The script is a Dobby variant with `WINKY_` prefix instead of `DOBBY_`. What makes it unique is not the code — it's the **deployment model**:

| | Dobby | Winky |
|---|---|---|
| **Who deploys** | Human (manually) | Butler AI (automated bootstrap) |
| **Where** | Local machine | Remote host (`/tmp/winky/`) |
| **Template** | Pre-existing in `dobby/` directory | Deployed by agent via `scp` |
| **Toolchain** | Pre-existing | Checked + installed by agent |
| **Tag choice** | Human at source time | Human at source time (agent never picks) |
| **After setup** | Identical experience | Identical experience |

### Comparison Across Remote Approaches

| | Stage 5 (SSH Sprawl) | Stage 6 (Winky) |
|---|---|---|
| **Audience** | Butler AI | Human |
| **Interface** | Raw `tell-me-go` calls via SSH stdin pipe | Interactive shell with `b`/`a`/`c`/`t`/`r` |
| **Session model** | Single-task sub-agent, then discard | Multi-turn interactive |
| **Workspace** | Ephemeral `/tmp/dobby.XXXXXX/` | Fixed `/tmp/winky/ait-<tag>/` |
| **Tag** | Auto-generated hex | Human-chosen |
| **Setup** | `eval $(ssh-sprawl-agent.sh ...)` per spawn | One-time bootstrap, then `source winky.sh` |

### What It Solves vs. Stage 5

| Limitation (Stage 5) | Stage 6 (Winky) |
|-----------------------|------------------|
| Human must manually set up remote env | Butler AI bootstraps everything |
| Headless — no interactive shell functions | Full Dobby-like interactive experience on remote |
| Ephemeral workspaces, discarded after task | Persistent workspace, human-managed sessions |

---

## Stage 7: Flopsy — Dynamic Role Aliases from Variations

All previous stages hard-code their role aliases: `b` for butler, `a` for architect, `c` for coder, `t` for tester, `r` for reviewer. To add a new role, you modify the script.

Flopsy eliminates this. Role aliases are **discovered at source time** from a `configs/variations/` directory. Add a YAML file — get a shell function. No script changes needed.

### How It Works

The `ait-base/configs/variations/` directory contains lightweight YAML files, each with just `MODE` and `PERSON`:

```yaml
# configs/variations/tony.yaml
MODE: "tony"
PERSON: "Your name is Tony, an AI assistant who..."

# configs/variations/helen.yaml
MODE: "helen"
PERSON: "Your name is Helen, an AI assistant who..."
```

When you run `source flopsy.sh -n`, Flopsy:

1. Scans `configs/variations/` for `*.yaml` files
2. Reads each file's `MODE` and `PERSON`
3. Takes the **first letter** of the filename as the shell alias — `tony.yaml` → `t`, `helen.yaml` → `h`
4. Generates a full config (`configs/tony.yaml`, `configs/helen.yaml`) by merging the variation's `MODE` + `PERSON` with `butler.yaml` (all providers, pricing, tools)
5. Defines shell functions dynamically via `eval`

### Validation Rules

Flopsy enforces two constraints at creation time:

- **`b` is reserved** for butler — no variation file can start with `b`
- **Unique first letters** — `tony.yaml` and `tester.yaml` can't coexist (both would claim `t`)

Violations abort creation and clean up the partial workspace.

### Usage

```bash
source flopsy.sh -n studio vertex-flash
# Scans variations/, discovers: tony→t, helen→h, jenny→j, micole→m

b "Hello!"   # Butler (always available)
t "Hello!"   # Tony (from variations/tony.yaml)
h "Hello!"   # Helen (from variations/helen.yaml)
j "Hello!"   # Jenny (from variations/jenny.yaml)
m "Hello!"   # Micole (from variations/micole.yaml)

# Mid-session provider switch — aliases survive:
source flopsy.sh openai
t "Same Tony, different model"
```

The summary on source shows all discovered commands:

```
Commands:
  b "prompt"        butler (assistant)
  t "prompt"        tony
  h "prompt"        helen
  j "prompt"        jenny
  m "prompt"        micole
```

### What It Solves vs. Stage 3 (Dobby)

| Limitation (Dobby) | Stage 7 (Flopsy) |
|--------------------|------------------|
| Roles are hard-coded (`b`/`a`/`c`/`t`/`r`) | Roles discovered from `variations/` directory |
| Adding a role = editing the script | Adding a role = dropping a YAML file into `variations/` |
| Fixed set of personas | Arbitrary named personas with custom MODE and PERSON |
| Alias collisions don't exist (fixed set) | Validation: no duplicate first letters, `b` reserved |

### The Evolution of Flexibility

| Stage | Role model | How to add a role |
|-------|-----------|-------------------|
| Stage 1 | Single `b` alias | Edit `.bashrc` |
| Stage 2–3 (Toby/Dobby) | Fixed `b`/`a`/`c`/`t`/`r` | Edit the script |
| Stage 7 (Flopsy) | `b` + any number of discovered aliases | Add a YAML file to `variations/` |

Flopsy is the endpoint of the role-flexibility arc: from one fixed role, to five fixed roles, to an arbitrary number of user-defined roles discovered at source time.
