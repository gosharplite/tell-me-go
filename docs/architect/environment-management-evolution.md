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
