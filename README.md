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
*   **Multi-Provider Reasoning**: Native support for Gemini 1.5/2.0/3.0, GPT-4o/o1/o3/5, DeepSeek R1, and Claude 3.5/3.7/4.
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
*   **Go**: 1.26.1 or higher.
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
[08:21:09] [google] M: 31882 H: 1204 C: 402 Th: 0 ($0.0123) [3.20s (ΣT: 0.15s) / 4.50s]
╰─⠿ Ready ($0.0123 $0.0123 $1.4745 $2.1050 M: 31882 H: 1204 3.6% O: 402)
```

## ⚙️ Configuration
```yaml
# configs/assistant.yaml
MODE: "assistant"
PERSON: "You are an AI assistant. Please respond concisely and accurately in English."

# --- Active Provider ---
SELECTED_PROVIDER: "google"

# --- Provider Registry ---
PROVIDERS:
  google:
    TYPE: "gemini"
    MODEL: "gemini-3-flash-preview"
    URL: "https://aiplatform.googleapis.com/v1/projects/${GOOGLE_PROJECT_ID}/locations/global/publishers/google/models"
    API_KEY: "${GOOGLE_APPLICATION_CREDENTIALS}"
    THINKING_BUDGET: 32768
    THINKING_LEVEL: "HIGH"
  openai:
    TYPE: "openai"
    MODEL: "gpt-5.2"
    URL: "https://api.openai.com/v1"
    API_KEY: "${OPENAI_API_KEY}"
    HEADERS:
      reasoning_effort: "high"

# --- Model Overrides ---
MODELS:
  gpt-5:
    MAX_THINKING_BUDGET: 65536
    CONTEXT_WINDOW: 200000
    PRICING:
      HIT: 0.175
      MISS: 1.75
      COMP: 14.00

# --- Tools & Features ---
SHOW_THOUGHTS: false
SHOW_TOOLS: true

# --- Global Timeouts ---
HTTP_TIMEOUT: 300

# --- Concurrent Execution ---
MAX_CONCURRENT_TOOLS: 5
TOOL_TIMEOUT: 300

# --- Safety & History ---
MAX_TURNS: 200
MAX_HISTORY_TOKENS: 180000
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

For advanced usage with piped multi-agent workflows and role-based review checklists, see [docs/user/piped-multi-agent-workflow.md](docs/user/piped-multi-agent-workflow.md), [docs/steps/review-by-roles.md](docs/steps/review-by-roles.md), and [docs/steps/reduce-cyclomatic-complexity.md](docs/steps/reduce-cyclomatic-complexity.md).

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
