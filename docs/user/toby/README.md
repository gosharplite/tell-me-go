# Toby: Environment Manager for tell-me-go

Toby is a Bash environment manager that enables rapid switching between different `tell-me-go` configurations, providers, and isolated working environments. It creates and manages `ait-<tag>-<provider>` directories from a shared template, sets up shell aliases, updates configuration files, and modifies your prompt to show the current context.

## Overview

When working with multiple LLM providers (Gemini, OpenAI, Claude, DeepSeek) and different task contexts (coding, reviewing, testing, architecture), Toby lets you maintain separate, isolated `tell-me-go` environments with their own history, configuration, and API keys. Each environment is a self-contained copy of the `ait-base` template, customized for a specific `tag` (e.g., `feature-x`, `experiment-1`) and `provider` (e.g., `vertex-claude`, `openai`).

---

## Quickstart

### 1. Initial Setup

Ensure you have the required tools installed:
- **Bash 4+** (standard on most Linux/macOS)
- **`yq`** (YAML processor, version 4.x)
- **`go`** (to install `tell-me-go`)
- **`fzf`** (optional but highly recommended for interactive selection)

### 2. Configure API Keys (Template)

Place your provider API-key files (e.g., service account JSONs) directly into the template's secrets directory:

```bash
cd <value>/tell-me-go/docs/user/toby
mkdir -p ait-base/secrets
# Place your key files here (e.g., cp ~/downloads/key.json ait-base/secrets/key.json)
```

Update `ait-base/configs/assistant.yaml` to reference these files using the **virtual `/ait/` path**. For example:
`API_KEY: "<value>/tell-me-go/docs/user/toby/ait/secrets/key.json"`

When you create a new environment, Toby automatically clones the template (including your keys) and rewrites the YAML paths to point to the local project's copy.

### 3. Source Toby in Your Shell

Add the following to your `.bashrc` or `.zshrc`:

```bash
# Path to where you placed toby.sh
export TOBY_BASE_DIR="<value>/tell-me-go/docs/user/toby"
source "$TOBY_BASE_DIR/toby.sh"
```

Then reload your shell: `source ~/.bashrc`

### 4. Create Your First Environment

Run Toby interactively and use the `-n` flag to create a missing directory:

```bash
source toby.sh -n
```

1. **Select tag**: Type a new name like `myproject`.
2. **Select provider**: Choose from the list (e.g., `vertex-deepseek`).

This creates `ait-myproject-vertex-deepseek/` and updates your prompt: `[myproject|vertex-deepseek] $`.

### 5. Install tell-me-go

```bash
c-install
```

### 6. Start Working

Use the role-specific helpers immediately:
- `b "Hello!"` (Assistant)
- `c "Write a Go function..."` (Coder)
- `a "Design this system..."` (Architect)

---

## Quick Reference (Cheatsheet)

### Commands & Options

| Command | Description |
|---------|-------------|
| `source toby.sh` | Interactive switch (requires existing env) |
| `source toby.sh -n` | Interactive switch, create if missing |
| `source toby.sh <tag> <provider>` | Switch to specific environment |
| `source toby.sh -n <tag> <provider>` | Force create and switch |

### Helper Functions

| Function | Role | Config Used |
|----------|------|-------------|
| `b` | Assistant | `assistant.yaml` |
| `c` | Coder | `coder.yaml` |
| `t` | Tester | `tester.yaml` |
| `r` | Reviewer | `reviewer.yaml` |
| `a` | Architect | `architect.yaml` |

### Installation Shortcuts

| Alias | Action |
|-------|--------|
| `c-install` | Install latest `tell-me-go` from GitHub |
| `c-install-dev` | Install from local source (`$TOBY_DEV_GO_SRC`) |

---

## Key Features

- **Interactive Selection**: Uses `fzf` for tags and providers with fallback to a numbered menu.
- **Isolated Contexts**: Each environment gets its own `output/` directory (history, tasks, safe-paths).
- **API-Key Retargeting**: Automatically copies keys from a shared folder and updates YAML paths.
- **Dynamic Configuration**: Sets `MODE`, `SELECTED_PROVIDER`, and `MAX_HISTORY_TOKENS` (based on model context window) automatically.
- **Shell Integration**: Visual prompt updates (`[tag|provider]`), exports `TELL_ME_HOME`, and sets up completions.

---

## Directory Layout

```text
toby/
├── toby.sh                      # The manager script
├── ait-base/                    # Source template
│   ├── configs/*.yaml           # Role configurations
│   ├── docs/skills/             # Skill/pattern files
│   ├── secrets/                 # Default/placeholder keys
│   └── output/                  # Initial output dir
└── ait-<tag>-<provider>/        # Isolated working environment
    ├── configs/                 # Customized for provider/tag
    ├── secrets/                 # Local copies of API keys
    └── output/                  # Project-specific history/tasks
```

---

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `TOBY_BASE_DIR` | Root directory of the system |
| `TOBY_TAG` | Current active tag |
| `TOBY_PROVIDER` | Current active provider |
| `TELL_ME_HOME` | Path to the active environment (used by `tell-me-go`) |
| `TELL_ME_GOBIN` | Path to `tell-me-go` binary |

---

## Common Workflows

### Multi-Agent Collaboration
Chain roles within the same project:
1. `a "Design the API"`
2. `c "Implement the design in Go"`
3. `r "Review the implementation"`
4. `t "Generate unit tests"`

### Provider Comparison
Compare the same prompt across different models:
1. `source toby.sh -n compare vertex-deepseek`
2. `b "Write a regex for emails"`
3. `source toby.sh -n compare openai`
4. `b "Write a regex for emails"`

### Maintenance
- **Update tool**: Run `c-install`.
- **Clean up**: Simply `rm -rf ait-old-project-*`.
- **Inspect**: `echo $TELL_ME_HOME` or `echo $TOBY_TAG`.

---

## Troubleshooting

- **"Must be sourced"**: Use `source toby.sh`, not `./toby.sh`.
- **"yq not found"**: Install `yq` version 4.
- **"Provider not in assistant.yaml"**: Add the provider definition to `ait-base/configs/assistant.yaml`.
- **Keys not working**: Verify they exist in `ait/secrets/` and that `assistant.yaml` points to them.
- **No completion**: Ensure `tell-me-go` is in your path and run `c-install`.

---

## See Also
- [tell-me-go Main README](../../../README.md)
- [Multi-Agent Workflow](../../piped-multi-agent-workflow.md)
