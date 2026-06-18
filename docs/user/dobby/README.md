# Dobby: Multi-Role Environment Manager for tell-me-go

Dobby is a Bash environment manager that creates isolated `tell-me-go` environments with dedicated configurations for every role in the SOP pipeline (**Architect → Coder → Reviewer → Tester → Butler**). Each `ait-<tag>` directory contains five auto-generated role configs, all derived from a single `butler.yaml` master source. Unlike Toby (which bakes the provider into the directory name), Dobby lets you switch AI providers on the fly within the same project — just re-source with the same tag and a different provider.

## Overview

Dobby is built for two complementary workflows: the **SOP multi-role pipeline** (where each role gets its own persona and config) and **mid-session provider switching** (comparing models, or escalating a tough problem from a flash to a pro model). Because the provider is selected at source time rather than encoded in the directory name, you keep the same `ait-<tag>` environment — with all its session history, authorized paths, and task state — and simply hot-swap the underlying LLM.

---

## Quickstart

### 1. Prerequisites

Ensure you have the required tools installed:
- **Bash 4+** (standard on most Linux/macOS)
- **[`yq`](https://github.com/mikefarah/yq)** (YAML processor, version 4.x)
- **[`fzf`](https://github.com/junegunn/fzf)** (fuzzy finder, for interactive selection)
- **`go`** (to install `tell-me-go`)

### 2. Configure Secrets (Template)

Place your API-key files directly into the template's secrets directory:

```bash
cd <value>/tell-me-go/docs/user/dobby
mkdir -p ait-base/secrets
# Copy your Google service account key
cp ~/downloads/key.json ait-base/secrets/key.json
```

Edit `ait-base/secrets/keys` with your API keys as exported environment variables:

```bash
# ait-base/secrets/keys
export OPENAI_API_KEY="sk-..."
export DEEPSEEK_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-..."
```

Edit `ait-base/configs/butler.yaml` to set your Vertex AI project IDs in the provider URLs and ensure the `API_KEY` fields point to `${DOBBY_HOME}/secrets/key.json` for Vertex providers. When Dobby provisions a new environment, it automatically resolves these paths.

### 3. Source Dobby in Your Shell

Add the following to your `.bashrc` or `.zshrc`:

```bash
# Path to where you placed dobby.sh
export DOBBY_BASE_DIR="<value>/tell-me-go/docs/user/dobby"
source "$DOBBY_BASE_DIR/dobby.sh" -n myproject vertex-flash
```

Then reload your shell: `source ~/.bashrc`

Or source interactively:

```bash
source "$DOBBY_BASE_DIR/dobby.sh" -n
```

1. **Select tag**: Type a new name like `myproject`.
2. **Select provider**: Choose from the list (e.g., `vertex-flash`).

This creates `ait-myproject/` and updates your prompt: `[myproject|vertex-flash] $`.

### 4. Install tell-me-go

```bash
c-install
```

### 5. Start Working

Use the role-specific helpers immediately:
- `b "Hello!"` (Butler — general assistant)
- `a "Design this system..."` (Architect — design review)
- `c "Write a Go function..."` (Coder — implementation, TDD)
- `r "Review the code..."` (Reviewer — code review)
- `t "Generate tests..."` (Tester — QA strategy)

---

## Quick Reference (Cheatsheet)

### Commands & Options

| Command | Description |
|---------|-------------|
| `source dobby.sh` | Interactive switch (requires existing env) |
| `source dobby.sh -n` | Interactive switch, create if missing |
| `source dobby.sh -n -p` | Interactive switch, create with Vertex priority headers |
| `source dobby.sh -n -c` | Interactive switch, create without `docs/` folder |
| `source dobby.sh <tag> <provider>` | Switch to specific environment |
| `source dobby.sh -n <tag> <provider>` | Force create and switch |

### Helper Functions

| Function | Role | Config Used | Access |
|----------|------|-------------|--------|
| `b` | Butler | `butler.yaml` | Full |
| `a` | Architect | `architect.yaml` | Read-only |
| `c` | Coder | `coder.yaml` | Write |
| `r` | Reviewer | `reviewer.yaml` | Read-only |
| `t` | Tester | `tester.yaml` | Read-only |

### Installation Shortcuts

| Alias | Action |
|-------|--------|
| `c-install` | Install latest `tell-me-go` from GitHub |

---

## Key Features

- **Multi-Role Environments**: Each `ait-<tag>` directory contains five auto-generated role configs, all derived from a single `butler.yaml` master config while preserving each role's unique persona.
- **Template-Based Provisioning** (`-n`): Clones `ait-base/` and generates `architect.yaml`, `coder.yaml`, `reviewer.yaml`, and `tester.yaml` from `butler.yaml`.
- **Priority Mode** (`-p`): Injects Vertex AI shared/priority headers (`X-Vertex-AI-LLM-Request-Type: shared`, `X-Vertex-AI-LLM-Shared-Request-Type: priority`) into all Vertex provider entries for higher-throughput quota pools. Uses `butler-priority.yaml` as the generation source.
- **Clean Mode** (`-c`): Skips copying the `docs/` folder for lean, disk-efficient environments.
- **Interactive Selection**: Uses `fzf` for tags and providers with support for typing new tag names.
- **Provider Flexibility**: The provider is selected at source time (not encoded in the directory name), so you can switch providers within the same environment.
- **Secrets Isolation**: Each environment has its own `secrets/` directory with `keys` (sourced as environment variables) and `key.json` (Google service account credentials).
- **Single-Sourcing Guard**: Prevents accidental double-sourcing; displays the current tag/provider if already active.
- **Shell Integration**: Visual prompt (`[tag|provider]`), exports `DOBBY_HOME` and `DOBBY_TAG`, and sets up Bash completions.

---

## Directory Layout

```text
dobby/
├── dobby.sh                       # The environment manager (source this)
├── ait-base/                      # Source template
│   ├── configs/
│   │   ├── butler.yaml            # Master config (all providers + pricing)
│   │   ├── butler-priority.yaml   # Variant with Vertex priority headers
│   │   ├── architect.yaml         # Architect persona (persons only)
│   │   ├── coder.yaml             # Coder persona (persons only)
│   │   ├── reviewer.yaml          # Reviewer persona (persons only)
│   │   └── tester.yaml            # Tester persona (persons only)
│   ├── secrets/
│   │   ├── key.json               # Google service account credentials
│   │   └── keys                   # Shell environment variables (sourced)
│   ├── docs/skills/               # Shared skill documentation
│   └── output/                    # Initial output directory
└── ait-<tag>/                     # Provisioned working environments
    ├── .gitignore                 # Ignores output/ and secrets/
    ├── configs/                   # Auto-generated from butler.yaml
    ├── secrets/                   # Copied from template
    └── output/                    # Isolated session data per environment
```

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `DOBBY_BASE_DIR` | Root directory containing `ait-base/` and `ait-<tag>/` environments |
| `DOBBY_HOME` | Path to the active environment (`ait-<tag>/`) |
| `DOBBY_TAG` | Current active tag |
| `DOBBY_PROVIDER` | Current active provider |
| `DOBBY_GOBIN` | Path to the `tell-me-go` binary |

When `tell-me-go` is invoked through the helper functions, the following are set automatically:

| Variable | Value |
|----------|-------|
| `TELL_ME_HOME` | `$DOBBY_HOME` |
| `TELL_ME_MODE` | `<role>-<tag>` (e.g., `coder-myproject`) |
| `TELL_ME_SELECTED_PROVIDER` | `$DOBBY_PROVIDER` |

---

## Common Workflows

### SOP Pipeline
Run the full multi-role pipeline within the same environment:
1. `a "Design the API architecture"`
2. `c "Implement the design in Go"`
3. `r "Review the implementation for security and idioms"`
4. `t "Verify test coverage and flag missing edge cases"`
5. `b "Summarize findings and next steps"`

### Provider Comparison
Compare the same prompt across different models within the same tag:
1. `source dobby.sh myproject vertex-flash`
2. `b "Write a regex for email validation"`
3. `source dobby.sh myproject openai`
4. `b "Write a regex for email validation"`

### Priority Mode for Batch Work
Use Vertex priority headers for higher-throughput quota when running large batch tasks:
```bash
source dobby.sh -n -p batch-job vertex-flash
c "Process all 500 records with the following logic..."
```

### Quick Role Handoff
Pass work between roles without changing environments:
1. `a "Review the current architecture for bottlenecks"` → Architect analyzes
2. `c "Implement the architect's recommendations"` → Coder writes code
3. `r "Audit the coder's changes"` → Reviewer validates

---

## Troubleshooting

- **"Must be sourced"**: Use `source dobby.sh`, not `./dobby.sh`.
- **"Already sourced"**: Dobby is already active. Open a new terminal (Ctrl+D) to change environments.
- **"yq not found"**: Install `yq` version 4.x from the [releases page](https://github.com/mikefarah/yq).
- **"fzf is required for interactive selection"**: Install `fzf` or pass `<tag>` and `<provider>` directly as arguments.
- **"Provider not in butler.yaml"**: Add the provider definition to `ait-base/configs/butler.yaml` under the `PROVIDERS` key.
- **Keys not working**: Verify `ait-base/secrets/key.json` exists and `ait-base/secrets/keys` exports the correct environment variables. For Vertex providers, ensure the API_KEY field in `butler.yaml` uses `${DOBBY_HOME}/secrets/key.json`.

---

## See Also
- [tell-me-go Main README](../../../README.md)
- [Toby Documentation](../toby/README.md) – Simpler single-role environment manager
- [Multi-Agent Workflow](../../piped-multi-agent-workflow.md)
