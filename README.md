<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->
<p align="center">
  <img src="assets/tell-me-go.png" alt="tell-me-go logo" width="250">
</p>

# tell-me-go: A Multi-Provider Reasoning Agent for the Terminal

A high-performance CLI assistant unifying the world's most powerful reasoning engines (**Gemini, OpenAI, DeepSeek, Claude, Kimi**) under a single, resilient interface.

## Overview
`tell-me-go` is a production-ready reasoning agent designed for complex developer workflows. By abstracting the complexities of diverse LLM providers (**Google Vertex AI, OpenAI, DeepSeek, Anthropic, Kimi-K3**) into a unified domain model, it provides a stable platform for tool-augmented intelligence and multi-turn reasoning. Built with the speed of Go, it prioritizes session durability through automated context maintenance, provides a rich TUI for history exploration, and prevents "hidden" expenses with deterministic cost auditing and safety guardrails.

## 🚀 Features
*   **Multi-Provider Reasoning**: Native support for Gemini 3.0/3.1, GPT-5.4/5.5, DeepSeek V3.2/V4, Kimi-K3, and Claude 4.7 — across Google Vertex AI, OpenAI, DeepSeek, Moonshot, and Anthropic APIs.
*   **Intelligence & Context**:
    *   **Skills.sh Ecosystem**: Discover, install, and remove skills on the fly from the open agent skills ecosystem. Skills are installed via `git clone` into `.skills/` and are available immediately. Requires user approval for installation.
    *   **Unified Domain Model**: Optimized for a provider-agnostic `Thought` architecture, ensuring consistent reasoning across models.
*   **Agentic Tools**: Natively executes local and cloud tools to solve complex tasks.
    *   **Workspace**: FileSystem (read/write/search/diff/undo), Git (status/diff/log/commit/branch/blame), and AST-powered Go Analysis (usages, definitions, symbols, rename/move refactoring).
    *   **Deep Analysis**: High-level tools for `verify_architecture` (Hexagonal/Clean Architecture checks), `get_code_health`, and `analyze_sequence_flow` (Mermaid sequence diagrams).
    *   **Enterprise**: Deep integration with **Jira** (search/get), **Confluence** (search/read/write), **Azure DevOps** (PRs, Repos, Pipeline creation/runs/logs), and **Teams**.
    *   **System & Dev**: Shell execution (`execute_command`, `pipe_commands`), testing, linting, and vulnerability scanning (`govulncheck`).
    *   **State & History**: Task tracking, `summarize_history`, `manage_history` (pinning/unpinning turns to protect them from pruning), and **Interactive History Browser** (`tell-me-go browse`) with full-text search and O(1) archive navigation.
    *   **Media**: Imagen 3 image generation and Vision analysis.
*   **Safety Guardrails**: 
    *   **Context Control**: Automatic "self-healing" summarization and turn pinning to prevent overflow without losing intent.
    *   **Runaway Protection**: Hallucination loop detection (SHA-256 response hashing), tool repetition guards, and recursion limits.
    *   **Access Security**: Path-based shell validation, persistent directory authorization (`register_safepath`, `register_readpath`), and secure API integration.
    *   **Resilience**: Real-time USD cost auditing, deterministic budgets, and descriptive API error handling.
*   **Persistence & Recovery**: 
    *   **Durability**: Automatic history saving with built-in **Auto-Repair** for crash resilience and session continuity.
    *   **Archiving**: New sessions (`--new`) archive history while preserving global state (tasks and `SafePath`).

## 🤖 AI Session Bootstrap

Before any AI-assisted work in this repository, an AI agent **must** read the
following files to establish a working understanding of the project:

| # | File / Command | Why |
|---|---|---|
| 1 | `README.md` | Project overview, features, configuration, usage |
| 2 | `Makefile` | Build/test conventions, coverage exclusions, architectural guard checks |
| 3 | `docs/domain-model/tell-me-go.modelith.md` | Canonical domain model — entities, relationships, invariants, scenarios |
| 4 | `docs/domain-model/quality.modelith.md` | Quality process model — `QualityPipeline` gates, coverage/complexity triage, `NonFixCatalog` curation rules |
| 5 | `docs/architect/environments/` | Environment management evolution and tooling (Toby, Dobby, etc.) |
| 6 | `docs/architect/environments/domain-model/environment-management.modelith.md` | Domain model for environment management concepts |
| 7 | `docs/architect/INTENTIONAL_NON_FIXES.md` | Known code patterns deliberately left as-is, with rationale |
| 8 | Execute `list_skills` | Discover installed skills available for the session |

## 📋 Prerequisites
*   **Go**: 1.26.4 or higher.
*   **Development Tools** (optional): For building from source, running tests, and contributing, install `golangci-lint`, `staticcheck`, `govulncheck`, `goimports`, and `gh` (GitHub CLI). See the Development section below.

## 🛠️ Installation

### Pre-built Binaries (no Go required)
Download the latest binary for your platform from [GitHub Releases](https://github.com/gosharplite/tell-me-go/releases):

**Linux (amd64):**
```bash
curl -L https://github.com/gosharplite/tell-me-go/releases/latest/download/tell-me-go-linux-amd64.tar.gz | tar xz
sudo mv tell-me-go-linux-amd64 /usr/local/bin/tell-me-go
```

**macOS (Apple Silicon):**
```bash
curl -L https://github.com/gosharplite/tell-me-go/releases/latest/download/tell-me-go-darwin-arm64.tar.gz | tar xz
sudo mv tell-me-go-darwin-arm64 /usr/local/bin/tell-me-go
```

**macOS (Intel):**
```bash
curl -L https://github.com/gosharplite/tell-me-go/releases/latest/download/tell-me-go-darwin-amd64.tar.gz | tar xz
sudo mv tell-me-go-darwin-amd64 /usr/local/bin/tell-me-go
```

**Windows (PowerShell):**
```powershell
Invoke-WebRequest -Uri https://github.com/gosharplite/tell-me-go/releases/latest/download/tell-me-go-windows-amd64.zip -OutFile tell-me-go.zip
Expand-Archive tell-me-go.zip -DestinationPath .
```

Linux ARM64 and Windows ARM64 binaries are also available on the [releases page](https://github.com/gosharplite/tell-me-go/releases).

### From Source (requires Go 1.26.4+)
```bash
go install github.com/gosharplite/tell-me-go/cmd/tell-me-go@latest
```

## 💻 Usage
Run the assistant by passing your prompt as an argument. By default, it uses `configs/assistant.yaml`.

**Basic Usage:**
```bash
tell-me-go "How to use this tool?"
```

**Interactive TUI Prompt (Auto-complete):**
Launch a rich terminal prompt with real-time suggestions for history, files, and tools.
```bash
tell-me-go -i
```

**History Management:**
Show the last 5 messages:
```bash
tell-me-go -l 5
```
Interactive history browsing with search and thought visibility:
```bash
tell-me-go browse
```
Go back / undo the last message (shorthand):
```bash
tell-me-go -b
```
Undo the last 3 turns from history:
```bash
tell-me-go -b 3
```
Retry the last user message (automatically rolls back the last attempt):
```bash
tell-me-go --retry
```
Edit the last model response (text and thinking) in an interactive TUI:
```bash
tell-me-go -e
```

**Session Logs:**
Print the full execution trace (turns log) for the current session and exit. This is useful for inspecting the detailed background process or debugging:
```bash
tell-me-go -t
```

**Raw Output Mode:**
Skip Markdown rendering (useful for piping to other scripts):
```bash
tell-me-go -r "Write a bash script to list files"
```

**TUI Progress Dashboard:**
Enable a real-time progress dashboard with live token usage and tool execution status during agent turns:
```bash
tell-me-go -o "Refactor the auth module"
```

**System Diagnostics:**
Run a comprehensive health check:
```bash
tell-me-go -d
```
Use `--json` with `-d` for machine-readable diagnostic output:
```bash
tell-me-go -d --json
```

**Pre-flight Status Log:**
Before every request, the tool shows your current resource usage relative to configured limits:
```text
────────────────────────────────────────────────────────────────────────────────
╭─⠿ Turn 3/20
[14:19:43] Payload: ~4549/1000000 tokens
```

**Post-turn Usage Metrics:**
Detailed cost and token breakdown after every response:
```text
[08:21:09] Payload: 33488/1000000 tokens
[08:21:09] [google] M: 31882 H: 1204 C: 402 ($0.0123) [3.20s (ΣT: 0.15s) / 4.50s]
╰─⠿ Ready ($0.0123 $0.0123 $1.4745 M: 31882 H: 1204 3.6% O: 402)
```

> **Note on the `Th:` (thinking-tokens) field**: When present in the metrics line, `Th: <n>` shows the number of reasoning tokens the model emitted on this turn. The segment is intentionally suppressed when `n == 0` so it never appears for non-reasoning turns or for providers that do not separately report reasoning tokens. **Anthropic Claude in particular never displays a `Th:` value** — its API rolls reasoning tokens into the standard output count (`output_tokens`) and bills them at the standard output rate, with no separate counter on the wire. To verify extended thinking is firing on the Anthropic path, set `SHOW_THOUGHTS: true` and you will see the model's reasoning rendered under `[Thinking]` blocks. Gemini and OpenAI/DeepSeek continue to show `Th: <n>` whenever reasoning fires.

## ⚙️ Configuration
```yaml
# configs/assistant.yaml
MODE: "assistant"
PERSON: "You are an AI assistant. Please respond concisely and accurately in English."

# --- Active Provider ---
SELECTED_PROVIDER: "deepseek-flash"

# --- Provider Registry ---
PROVIDERS:
  deepseek-flash:
    TYPE: "deepseek"
    MODEL: "deepseek-v4-flash"
    URL: "https://api.deepseek.com"
    API_KEY: "${DEEPSEEK_API_KEY}"
    MAX_TOKENS: 32768
  deepseek-pro:
    TYPE: "deepseek"
    MODEL: "deepseek-v4-pro"
    URL: "https://api.deepseek.com"
    API_KEY: "${DEEPSEEK_API_KEY}"
    MAX_TOKENS: 32768
  vertex-deepseek:
    TYPE: "deepseek"
    MODEL: "deepseek-ai/deepseek-v3.2-maas"
    URL: "https://aiplatform.googleapis.com/v1beta1/projects/${GOOGLE_PROJECT_ID}/locations/global/endpoints/openapi"
    API_KEY: "${GOOGLE_APPLICATION_CREDENTIALS}"
  vertex-flash:
    TYPE: "gemini"
    MODEL: "gemini-3-flash-preview"
    URL: "https://aiplatform.googleapis.com/v1/projects/${GOOGLE_PROJECT_ID}/locations/global/publishers/google/models"
    API_KEY: "${GOOGLE_APPLICATION_CREDENTIALS}"
    THINKING_BUDGET: 32768
    THINKING_LEVEL: "HIGH"
    MAX_TOKENS: 40960
  vertex-pro:
    TYPE: "gemini"
    MODEL: "gemini-3.1-pro-preview"
    URL: "https://aiplatform.googleapis.com/v1/projects/${GOOGLE_PROJECT_ID}/locations/global/publishers/google/models"
    API_KEY: "${GOOGLE_APPLICATION_CREDENTIALS}"
    THINKING_BUDGET: 32768
    THINKING_LEVEL: "HIGH"
    MAX_TOKENS: 40960
  vertex-claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-7"
    URL: "https://aiplatform.googleapis.com/v1/projects/${GOOGLE_PROJECT_ID}/locations/global/publishers/anthropic/models"
    API_KEY: "${GOOGLE_APPLICATION_CREDENTIALS}"
    THINKING_BUDGET: 32768
  openai:
    TYPE: "openai"
    MODEL: "gpt-5.5"
    URL: "https://api.openai.com/v1"
    API_KEY: "${OPENAI_API_KEY}"
    HEADERS:
      reasoning_effort: "high"
  claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-7"
    URL: "https://api.anthropic.com/v1"
    API_KEY: "${ANTHROPIC_API_KEY}"
    THINKING_BUDGET: 32768

# --- Tools & Features ---
USE_SEARCH: false
SHOW_THOUGHTS: false
SHOW_TOOLS: true

# --- Global Timeouts & Streaming ---
HTTP_TIMEOUT: 300

# --- Concurrent Execution ---
MAX_CONCURRENT_TOOLS: 5
TOOL_TIMEOUT: 300

# --- Safety & History ---
# Set to true to disable all interactive security prompts (ideal for automated workflows)
# BYPASS_CONFIRMATION: false
MAX_TURNS: 1000
MAX_HISTORY_TOKENS: 1000000

# --- Model Overrides ---
MODELS:
  "deepseek-v4-flash":
    CONTEXT_WINDOW: 1000000
    PRICING:
      HIT:      0.0028
      MISS:     0.14
      COMP:     0.28
  "deepseek-v4-pro":
    CONTEXT_WINDOW: 1000000
    PRICING:
      HIT:      0.003625
      MISS:     0.435
      COMP:     0.87
  "deepseek-ai/deepseek-v3.2-maas":
    CONTEXT_WINDOW: 163840
    PRICING:
      HIT:      0.056
      MISS:     0.56
      COMP:     1.68
  "gemini-3-flash-preview":
    CONTEXT_WINDOW: 200000
    PRICING:
      HIT: 0.09
      MISS: 0.90
      COMP: 5.40
  "gemini-3.1-pro-preview":
    CONTEXT_WINDOW: 200000
    PRICING:
      HIT: 0.36
      MISS: 3.60
      COMP: 21.60
  "gpt-5.4":
    CONTEXT_WINDOW: 200000
    PRICING:
      HIT: 0.25
      MISS: 2.50
      COMP: 15.00
  "gpt-5.5":
    CONTEXT_WINDOW: 200000
    PRICING:
      HIT: 0.50
      MISS: 5.00
      COMP: 30.00
  "claude-opus-4-7":
    CONTEXT_WINDOW: 200000
    PRICING:
      HIT: 0.50
      MISS: 6.25
      COMP: 25.00
```

## ⌨️ Shell Integration (Recommended)
Choose the configuration that matches your operating system and shell.

### Bash/Zsh (Linux/macOS)
Add these to your `.bashrc` or `.zshrc`:

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

### PowerShell (Windows)
Add these to your PowerShell profile (run `notepad $PROFILE` to edit):

```powershell
# 1. Set the home directory for session history and global state
$env:TELL_ME_HOME = "$HOME\tell-me-go"

# 2. Main command functions with piping support
function b {
    if ($MyInvocation.ExpectingInput) { $input | & tell-me-go.exe -c "$env:TELL_ME_HOME\configs\assistant.yaml" @args }
    else { & tell-me-go.exe -c "$env:TELL_ME_HOME\configs\assistant.yaml" @args }
}

# 3. Quick session undo
function bb { b -b @args }

# 4. Show last 10 messages
function bl { b -l 10 @args }

# 5. Start fresh session
function b-new { b --new @args }

# 6. Maintenance: Update and install
function b-install {
    Push-Location $env:TELL_ME_HOME
    git pull
    go install .\cmd\tell-me-go
    Pop-Location
}
```

For advanced usage with piped multi-agent workflows and role-based review checklists, see [docs/user/piped-multi-agent-workflow.md](docs/user/piped-multi-agent-workflow.md), [docs/steps/review-by-roles.md](docs/steps/review-by-roles.md), and [docs/steps/reduce-cyclomatic-complexity.md](docs/steps/reduce-cyclomatic-complexity.md). For the fully-automated Architect + Coder Issue-to-PR pipeline, see [docs/sop/lifecycle/issue_to_pr_orchestration.md](docs/sop/lifecycle/issue_to_pr_orchestration.md).

### Advanced Environment Management with Niffler

If you need fully isolated AI environments with category-level templates (e.g., a software engineering pipeline vs. a creative writing studio), consider using **Niffler** – the culmination of tell-me-go's environment management evolution. Niffler provisions isolated `tell-me-go` environments from self-contained **group templates** under `ait-base/`, with on-the-fly provider switching and dynamic persona discovery.

Niffler provides:
- **Group‑based templates** (`ait-base/<group>/`) — each group is a self-contained category with its own configs, secrets, personas, and optional skills/docs
- **Provider‑agnostic environments** (`ait‑<tag>` directories) — the provider is chosen at source time, not baked into the directory name, enabling mid-session hot‑swap
- **Dynamic persona discovery** — shell aliases are derived from the first letter of each persona YAML file in the group's `configs/`; add a file, get a function. No script changes needed
- **Per‑group skills** — the `docs/skills/` directory is optional per group (both current groups ship `tmg-chat-ingroup`; the earlier Toby/Dobby templates additionally carry `golang-patterns`, `golang-testing`, `k8s-operations`)
- **Template‑based provisioning** (`-n` flag) — auto‑generates full role configs by merging persona stubs (MODE + PERSON) with `butler.yaml`
- **Priority mode** (`-p` flag) for Vertex AI shared/priority headers
- **Clean mode** (`-c` flag) for lean, docs‑free environments
- **Interactive selection** (with `fzf`) for groups, tags, and providers
- **Role‑specific helper functions** (`b`, `a`, `c`, `t`, `r` …) scoped to the current environment
- **Visual shell prompt** showing the active tag and provider

See the [Niffler documentation](docs/user/niffler/niffler.sh) for installation and usage. For the full evolution from bash aliases through Niffler, see [Evolution of Environment Management](docs/architect/environments/environment-management-evolution.md).

## 🛠️ Development

To build from source, run tests, or contribute to `tell-me-go`, install the following Go development tools:

```bash
# Install linting and analysis tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install golang.org/x/tools/cmd/goimports@latest
```

It is also recommended to install the **GitHub CLI (`gh`)** to allow the agent to manage issues, review PRs, and automate GitHub operations natively from the terminal:
```bash
# macOS
brew install gh

# Ubuntu / Debian
sudo apt install gh
```

To work with the domain model, install **modelith**. The modelith binary used
across this ecosystem is built from the
[`gosharplite/modelith`](https://github.com/gosharplite/modelith) fork at the
`feat/self-domain-model` branch — a fork of
[`stacklok/modelith`](https://github.com/stacklok/modelith). Build it from a
checkout of that branch (`task build` / `go install`) rather than installing
the upstream release, or behavior may differ:

```bash
go install github.com/gosharplite/modelith/cmd/modelith@feat/self-domain-model
```

Then run the standard Go workflow:

```bash
# Build the binary
make build

# Run all tests
make test

# Format code
make fmt
```

See [ADR-036: Test Determinism Standards](docs/adr/2026-05-test-determinism-standards.md) for project-wide testing conventions (no `time.Sleep`, race-safe mocks, deterministic time).

See the [Makefile](Makefile) for additional targets.

## 🗺️ Domain Model

tell-me-go maintains a canonical **[domain model](docs/domain-model/)** — a
plain-language description of what the system *is*: its entities
(`Session`, `Turn`, `Provider`, `Tool`, …), their relationships, the invariants
that govern them, and scenario narratives that stress-test the whole.

The model is authored as a `*.modelith.yaml` file and rendered to Markdown with
embedded Mermaid ER diagrams using **modelith** — the
[`gosharplite/modelith`](https://github.com/gosharplite/modelith) fork at
`feat/self-domain-model`, a fork of the [Stacklok](https://stacklok.com)
[modelith](https://github.com/stacklok/modelith) domain-model-as-code
toolchain. The Markdown is regenerated and checked for drift in CI
(`make modelith-check`).

```sh
make modelith-lint     # validate the YAML
make modelith-render   # regenerate the Markdown
make modelith-check    # CI gate: fail if the committed .md is stale
```

See [docs/domain-model/README.md](docs/domain-model/README.md) for conventions,
the 3-pass build order, and design rationale.

## Design Decisions
Significant architectural decisions are documented in our [Architecture Decision Records (ADRs)](docs/adr/README.md).

*   **[ADR-053](docs/adr/2026-08-remove-get-cost-summary-and-daily-cost-metric.md)** — removal of the `get_cost_summary` tool, the `DailyCost` status-line metric, and the global cost ledger.
*   **[ADR-057](docs/adr/2026-08-remove-context-window-cache.md)** — removal of the context window cache from the session context manager (`internal/agent/session/context`).
*   **[ADR-058](docs/adr/2026-08-getwindow-clone-evaluation.md)** — acceptance of the per-round `GetWindow` deep clone as the sole remaining O(window) cost; F2 evaluation closes at candidate 3 (accept-with-data) for both the cheaper-clone and prune-then-clone candidates, continuing the ADR-057 removal lineage (issue #1321).

For the evolution of the shell-based environment management tooling — from simple bash aliases through Toby, Dobby, Porter, Sprawl, Winky, Flopsy, and Niffler — see [Evolution of Environment Management](docs/architect/environments/environment-management-evolution.md).

**Before proposing structural changes or flagging potential issues**, consult these two files first — they are the authoritative source for project conventions and known anomalies:

- **[Makefile](Makefile)** — Defines all build/test conventions, coverage exclusions (including `*test/` and `testing/` directories), and architectural guard checks (`verify-architecture`, `verify-no-test-sleep`, `verify-testutil-convention`, etc.).
- **[Intentional Non-Fixes](docs/architect/INTENTIONAL_NON_FIXES.md)** — Catalog of known code patterns and directories deliberately left as-is, with rationale for each. Prevents re-investigation of already-settled decisions.

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
