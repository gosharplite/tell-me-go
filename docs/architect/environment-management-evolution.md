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
