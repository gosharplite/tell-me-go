<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->
<p align="center">
  <img src="assets/tell-me-go.png" alt="tell-me-go logo" width="250">
</p>

# tell-me-go: A Multi-Provider Reasoning Agent for the Terminal

A high-performance CLI assistant unifying the world's most powerful reasoning engines (**Gemini, OpenAI, DeepSeek, Claude**) under a single, resilient interface.

## Overview
`tell-me-go` is a production-ready reasoning agent designed for complex developer workflows. By abstracting the complexities of diverse LLM providers (**Google Vertex AI, OpenAI, DeepSeek, Anthropic**) into a unified domain model, it provides a stable platform for tool-augmented intelligence and multi-turn reasoning. Built with the speed of Go, it prioritizes session durability through automated context maintenance, provides a rich TUI for history exploration, and prevents "hidden" expenses with deterministic cost auditing and safety guardrails.

## 🚀 Features
*   **Multi-Provider Reasoning**: Native support for Gemini 3.0/3.1, GPT-5.4/5.5, DeepSeek V3.2/V4, and Claude 4.7 — across Google Vertex AI, OpenAI, DeepSeek, and Anthropic APIs.
*   **Intelligence & Context**:
    *   **Dynamic Skill Injection**: Automatically injects idiomatic Go patterns (`golang-patterns`) and TDD best practices (`golang-testing`) into the context based on task relevance.
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
    *   **Archiving**: New sessions (`-new`) archive history while preserving global state (tasks and authorized paths).

## 📋 Prerequisites
*   **Go**: 1.26.3 or higher.
*   **Development Tools** (optional): For building from source, running tests, and contributing, install `golangci-lint`, `staticcheck`, `govulncheck`, `goimports`, and `gh` (GitHub CLI). See the Development section below.

## 🛠️ Installation
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

**System Diagnostics:**
Run a comprehensive health check:
```bash
tell-me-go -d
```

**Pre-flight Status Log:**
Before every request, the tool shows your current resource usage relative to configured limits:
```text
────────────────────────────────────────────────────────────────────────────────
╭─⠿ Session: 3/20 turns
[14:19:43] Payload: ~4549/180000 tokens
```

**Post-turn Usage Metrics:**
Detailed cost and token breakdown after every response:
```text
[08:21:09] Payload: 33488/180000 tokens
[08:21:09] [google] M: 31882 H: 1204 C: 402 ($0.0123) [3.20s (ΣT: 0.15s) / 4.50s]
╰─⠿ Ready ($0.0123 $0.0123 $1.4745 $2.1050 M: 31882 H: 1204 3.6% O: 402)
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

### Advanced Environment Management with Toby

If you work with multiple LLM providers or need isolated configuration/history per project, consider using **Toby** – a Bash environment manager that creates and switches between isolated `tell-me-go` environments.

Toby provides:
- **Tag‑based environments** (`ait‑<tag>‑<provider>` directories)
- **Interactive selection** (with `fzf`)
- **Automatic configuration updates** and API‑key path rewriting
- **Role‑specific helper functions** (`c`, `t`, `r`, `a`, `b`) scoped to the current environment
- **Visual shell prompt** showing the active tag and provider

See the [Toby documentation](docs/user/toby/README.md) for installation and usage.

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

Then run the standard Go workflow:

```bash
# Build the binary
make build

# Run all tests
make test

# Format code
make fmt
```

See the [Makefile](Makefile) for additional targets.

## Design Decisions
Significant architectural decisions are documented in our [Architecture Decision Records (ADRs)](docs/adr/README.md).

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
