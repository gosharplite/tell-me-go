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
`tell-me-go` is a production-ready reasoning agent designed for complex developer workflows. By abstracting the complexities of diverse LLM providers (**Google Vertex AI, OpenAI, DeepSeek, Anthropic**) into a unified domain model, it provides a stable platform for tool-augmented intelligence and multi-turn reasoning. Built with the speed of Go, it prioritizes session durability through automated context maintenance and prevents "hidden" expenses with deterministic cost auditing and safety guardrails.

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
    *   **State & History**: Scratchpad, task tracking, `summarize_history`, and `manage_history` (pinning/unpinning turns to protect them from pruning).
    *   **Media**: Imagen 3 image generation and Vision analysis.
*   **Safety Guardrails**: 
    *   **Context Control**: Automatic "self-healing" summarization and turn pinning to prevent overflow without losing intent.
    *   **Runaway Protection**: Hallucination loop detection (SHA-256 response hashing), tool repetition guards, and recursion limits.
    *   **Access Security**: Path-based shell validation, persistent directory authorization (`register_safepath`, `register_readpath`), and secure API integration.
    *   **Resilience**: Real-time USD cost auditing, deterministic budgets, and descriptive API error handling.
*   **Persistence & Recovery**: 
    *   **Durability**: Automatic history saving with built-in **Auto-Repair** for crash resilience and session continuity.
    *   **Archiving**: New sessions (`-new`) archive history while preserving global state (tasks, scratchpads, and authorized paths).

## 📋 Prerequisites
*   **Go**: 1.26 or higher.

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

**History Management:**
Show the last 5 messages:
```bash
tell-me-go -l 5
```
Go back / undo the last message (shorthand):
```bash
tell-me-go -b
```
Undo the last 3 turns from history:
```bash
tell-me-go -b 3
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
USE_SEARCH: false
SHOW_THOUGHTS: false
SHOW_TOOLS: true

# --- Global Timeouts & Streaming ---
HTTP_TIMEOUT: 300
DISABLE_STREAMING: false

# --- Concurrent Execution ---
MAX_CONCURRENT_TOOLS: 5
TOOL_TIMEOUT: 300

# --- Safety & History ---
MAX_TURNS: 200
MAX_HISTORY_TURNS: 500
MAX_HISTORY_TOKENS: 180000
```

## ⌨️ Shell Integration (Recommended)
Add these aliases to your `.bashrc` or `.zshrc` for a streamlined workflow:

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
alias b-new='b -new'
# 6. Maintenance: Update and install
alias b-install='(cd $TELL_ME_HOME && git pull && go install ./cmd/tell-me-go)'
```

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).

## 🧩 Attributions
This project includes adapted content from the following open-source resources:
*   **[everything-claude-code](https://github.com/affaanmustafa/everything-claude-code)**: Skill definitions in `docs/skills/` (see `docs/skills/NOTICE.md` for details).

## 🛠️ Development

### Standard Tasks
Use the `Makefile` for all development tasks:
- **Tidy Dependencies:** `make tidy`
- **Run Tests:** `make test`
- **Race Detection:** `make test-race` (Package-by-package execution for AI environments).
- **Coverage Profile:** `make test-coverage` (Excludes mocks/generated code).

### Architecture Documentation
Refer to the following documents for deep-dives into the system design:
- **ADR-002**: [Tool Execution Concurrency & Timeouts](docs/adr/0002-tool-execution-concurrency-and-timeouts.md)
- **ADR-003**: [Domain Decomposition Strategy](docs/adr/0003-domain-decomposition-strategy.md)
- **ADR-005**: [Skill Injection Architecture](docs/adr/0005-skill-injection-architecture.md)
- **SOPs**: Standard Operating Procedures are located in `docs/sop/`.
