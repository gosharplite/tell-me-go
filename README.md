<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# tell-me-go: A Multi-Provider Reasoning Agent for the Terminal

A high-performance, type-safe CLI assistant unifying the world's most powerful reasoning engines (**Gemini, OpenAI, DeepSeek, Claude**) under a single, resilient interface.

## Overview
`tell-me-go` is a production-ready reasoning agent designed for complex developer workflows. By abstracting the complexities of diverse LLM providers (**Google Vertex AI, OpenAI, DeepSeek, Anthropic**) into a unified domain model, it provides a stable platform for tool-augmented intelligence and multi-turn reasoning. Built with the speed of Go, it prioritizes session durability through automated context maintenance and prevents "hidden" expenses with deterministic cost auditing and safety guardrails.

## 🚀 Features
*   **Multi-Provider Reasoning**: Support for high-performance reasoning models.
*   **Unified Domain Model**: Optimized for a provider-agnostic `Thought` architecture.
*   **Agentic Tools**: Natively executes a vast library of local tools to solve complex tasks.
    *   **FileSystem (Workspace)**: `list_files`, `get_tree`, `read_file`, `write_file`, `append_text`, `search_files`, `replace_text`, `find_file`, `get_file_diff`, `undo_file_change`.
    *   **Intelligence (AST-Powered/Analysis)**: `find_usages`, `find_definitions`, `list_symbols`, `list_implementations`, `get_type_info`, `get_project_summary`, `get_file_skeleton`, `search_usages_globally`, `get_semantic_diff`, `rename_symbol`, `list_todos`, `go_doc`, `get_complexity_metrics`, `get_package_graph`, `generate_mermaid_diagram`, `move_definition`, `verify_architecture`, `get_code_health`, `get_detailed_coverage`, `analyze_sequence_flow`, `dead_code_graph`, `get_definitions`.
    *   **Git (Workspace)**: `get_git_status`, `get_git_diff`, `get_git_log`, `get_git_show`, `get_git_blame`, `git_commit`, `git_create_branch`.
    *   **Media & Vision (Integrations)**: `create_image` (Imagen 3), `read_image` (Vision).
    *   **State & Session**: `manage_scratchpad`, `manage_tasks`, `manage_config`, `get_session_info`, `summarize_history`, and `manage_history`.
    *   **External Integration (Integrations)**:
        *   **Atlassian Stack (Jira & Confluence)**: Enterprise-grade integration with centralized execution and **exponential back-off** for high reliability.
            *   **Jira**: `jira_search_issues` and `jira_get_issue` for discovering and retrieving issues. Supports **high-volume discovery** with configurable search limits.
            *   **Confluence**: `confluence_search`, `confluence_read`, `confluence_write` for searching, reading, and updating Confluence pages with automatic Markdown/XHTML conversion and **search depth control**.
        *   **Azure DevOps (ADO)**: Comprehensive integration for managing Pull Requests, Pipelines, and Repository items.
            *   **Pull Requests**: `ado_list_pull_requests`, `ado_get_pull_request`, `ado_get_pr_diff`, `ado_get_pr_threads`, `ado_get_pr_statuses`, `ado_get_pr_policy_evaluations`.
            *   **Pipelines & Builds**: `ado_list_pipeline_runs`, `ado_get_pipeline_run`, `ado_get_pipeline_logs`, `ado_get_build_timeline`, `ado_get_task_log`, `ado_get_build_changes`.
            *   **Repositories & Branching**: `ado_list_repository_items`, `ado_get_file_content`, `ado_list_branch_policies`.
        *   **Teams**: `send_teams_message` sends rich Adaptive Cards to Microsoft Teams channels via Power Automate webhooks.
        *   **Network**: `http_request`, `read_external_docs`.
    *   **System**: `execute_command`, `pipe_commands`, `ask_user`, `register_safepath`, `list_safepaths`, `remove_safepath`, `register_readpath`, `list_readpaths`, `remove_readpath`, `bypass_confirmation`, `revoke_bypass`.
    *   **Dev Tools (Developer)**: `run_tests`, `go_tidy`, `get_coverage`, `run_linter`, `run_benchmark`, `check_vulnerabilities`, `verify_release_readiness`.
    *   **Financial Metrics (Dynamic Pricing)**: 
        *   `estimate_cost`: Provides a detailed session cost breakdown using live-synced Vertex AI rates.
        *   `get_cost_summary`: Generates a daily expenditure report from a local persistent ledger (`output/global_costs.json`).
*   **Safety Guardrails**: 
    *   **Automatic Maintenance (Self-Healing)**: Prevents "Context Overflow" by automatically summarizing older unpinned conversation turns when the payload exceeds 90% of `MAX_HISTORY_TOKENS`. This relieves pressure without losing the semantic core of the session.
    *   **Turn-Aligned Pinning**: Critical instructions and user-selected turns can be **pinned** (using `manage_history`) to protect them from pruning or summarization, ensuring they remain in the model's active context.
    *   **Clogged Context Detection**: If the history becomes "clogged" with pinned turns and maintenance cannot reduce the size below 85% of capacity, the agent injects an **URGENT SYSTEM NOTICE** instructing the model to persist state and stop before a hard rollback occurs.
    *   **Infinite Loop Protection**: 
        *   **SHA-256 Hashing**: Detects and breaks "Hallucination Loops" by hashing the model's full response (Thought + Text + Tools).
        *   **Repetition Guard**: Tracks identical tool calls with same arguments to prevent runaway execution cycles.
    *   **Internal Hard Budget**: Enforces a deterministic USD limit (Safe-by-Design) to halt sessions automatically if costs exceed safety thresholds.
    *   **Recursion Limit**: Prevents excessive turns using the `MAX_TURNS` configuration.
    *   **Descriptive Error Handling**: Automatically identifies and reports specific API block reasons (e.g., `SAFETY`, `RECITATION`) instead of generic "empty response" errors.
    *   **Command Safety**: `execute_command` includes a path-based validation gate. Commands that access files outside the working directory (e.g., `cat /etc/passwd`) require manual confirmation, even for whitelisted "safe" commands.
    *   **Secure Integrations**: Standardized URL construction and idiomatic error handling across Confluence, Jira, and Teams integrations to prevent injection and data leakage.
    *   **Persistent Authorization**: The `register_safepath` tool allows you to permanently authorize specific directories or files outside the project root. This requires a double-confirmation handshake for maximum security.
*   **Session Persistence & Archiving**: Automatically remembers conversation history. When starting a new session (`-new`), session-specific data (history and logs) is archived to `output/backups/`, while environment state (safepaths, tasks, and scratchpads) remains persistent.
    *   **Crash Resilience**: Automatically persists history after every turn (user prompt, model response, and tool results). If a system crash occurs during execution, a built-in **Auto-Repair** mechanism fixes the history on next startup to ensure the session remains valid and resumable.

## 📋 Prerequisites
*   **Go**: 1.25 or higher.

## 🛠️ Installation
1.  **Clone the repository**:
    ```bash
    git clone https://github.com/gosharplite/tell-me-go.git
    cd tell-me-go
    ```
2.  **Build the binary**:
    ```bash
    go build -o tell-me-go ./cmd/tell-me-go/
    ```

## 💻 Usage
Run the assistant by passing your prompt as an argument. By default, it uses `configs/assistant.yaml`.

**Basic Usage:**
```bash
./tell-me-go "What is the capital of France?"
```

**List History:**
Show the last 5 messages:
```bash
./tell-me-go -l 5
```
Show the very last message (shorthand):
```bash
./tell-me-go -l
```
Show raw output (skip Markdown rendering):
```bash
./tell-me-go -l -r
```

**Show Version:**
```bash
./tell-me-go -v
```

**Raw Output Mode:**
You can also use the `-r` flag with a regular prompt to receive raw text without formatting:
```bash
./tell-me-go -r "Write a bash script to list files"
```

**Pre-flight Status Log:**
Before every request, the tool shows your current resource usage relative to configured limits:
```text
╭─⠿ Session: 3/20 turns
[14:19:43] Payload: ~4549/120000 tokens
```
*   **Session**: Current turn count / Max history turns.
*   **Payload**: Estimated payload size / Max history tokens.

**Post-turn Usage Metrics:**
After the model responds, a detailed breakdown is provided:
```text
[08:21:09] M: 31882 H: 1204 C: 402 Th: 0 [3.20s / 4.50s]
╰─⠿ Ready ($0.0123 $0.0542 $1.4745 $2.1050 M: 31882 H: 1204 3.6% O: 402)
```
*   **M (Misses)**: Paid tokens (Input minus Hits).
*   **H (Hits)**: Tokens served for free from Google's cache.
*   **C (Completion)**: Response tokens generated by the model.
*   **Th (Thinking)**: Thinking tokens used.
*   **[Time]**: `Model Latency / Total Turn Time`.
*   **($... $... $... $...)**: `Turn / Task / Session / Daily` costs.
*   **M / H / %**: Aggregate Session Misses, Hits, and Hit Rate.
*   **O (Output)**: Aggregate Session Output tokens.

**New Session:**
```bash
./tell-me-go -new "Start a fresh conversation."
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
    API_KEY: "${GOOGLE_APPLICATION_CREDENTIALS}" # Optional - fall back to gcloud auth
    THINKING_BUDGET: 32768
    THINKING_LEVEL: "HIGH"
  openai:
    TYPE: "openai"
    MODEL: "gpt-5.2"
    URL: "https://api.openai.com/v1"
    API_KEY: "${OPENAI_API_KEY}"
    HEADERS:
      reasoning_effort: "high"
  deepseek:
    TYPE: "deepseek"
    MODEL: "deepseek-reasoner"
    URL: "https://api.deepseek.com"
    API_KEY: "${DEEPSEEK_API_KEY}"
  claude:
    TYPE: "anthropic"
    MODEL: "claude-opus-4-6"
    URL: "https://api.anthropic.com/v1"
    API_KEY: "${ANTHROPIC_API_KEY}"
    THINKING_BUDGET: 32768

# --- Tools & Features ---
USE_SEARCH: false
SHOW_THOUGHTS: false
SHOW_TOOLS: true

# --- Concurrent Execution ---
MAX_CONCURRENT_TOOLS: 5
TOOL_TIMEOUT: 300

# --- Safety & History ---
MAX_TURNS: 200
MAX_HISTORY_TURNS: 500
MAX_HISTORY_TOKENS: 120000
```

## ⌨️ Shell Integration (Recommended)
To streamline your workflow, add these aliases to your `.bashrc` or `.zshrc`. This provides a fast, one-letter command (`b`) for interacting with the assistant and simplifies session management.

```bash
# 1. Set the home directory for session history, tasks, and scratchpad
export TELL_ME_HOME="$HOME/tell-me-go"

# 2. Main command alias (points to your preferred config)
# Using a function allows passing flags and prompts easily
b() {
    tell-me-go -c "$TELL_ME_HOME/configs/assistant.yaml" "$@"
}
export -f b

# 3. Start a fresh session (archives old history)
alias b-new='b -new'

# 4. Show the last N messages from history
# Usage: bl 5
alias bl='b -l'

# 5. Maintenance: Update from GitHub or install local dev changes
alias b-install='(cd $TELL_ME_HOME && git pull && go install ./cmd/tell-me-go)'
alias b-install-dev='go install ./cmd/tell-me-go'
```

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
