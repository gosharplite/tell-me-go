<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# tell-me-go: A Go Gemini CLI Assistant

A lightweight, terminal-based interface for Google's Gemini models, powered by the official Google GenAI SDK.

## Overview
`tell-me-go` is a high-performance, type-safe assistant designed for developers. Originally built for Google's Gemini models, it is now a **production-ready multi-provider reasoning agent** that unifies the world's most powerful Chain-of-Thought models (Gemini, DeepSeek, Claude) under a single, resilient interface.

## 🚀 Features
*   **Multi-Provider Reasoning**: Support for high-performance reasoning models including **DeepSeek-R1**, **Claude 3.7**, and **Gemini 2.0/3.0**.
*   **Unified Domain Model**: Optimized for rich reasoning content with a provider-agnostic `Thought` architecture.
*   **Official SDK & REST**: Built on `google.golang.org/genai` with native REST support for OpenAI-compatible (GPT-4o/5.2, DeepSeek-R1) and Anthropic (Claude 3.5/3.7/4) endpoints.
*   **Agentic Tools**: Natively executes a vast library of local tools and Google Search to solve complex tasks.
    *   **UI Controls**: Configurable visibility for thought processes (`SHOW_THOUGHTS`) and tool execution logs (`SHOW_TOOLS`) for a cleaner terminal experience.
    *   **Interactive Safety**: 
        *   **Serialized Prompts**: Tool headers are sequenced to prevent parallel execution logs from garbling interactive prompts.
        *   **Persistent Bypass**: The `bypass_confirmation` state persists across executions in the same mode. No more re-authorizing every run.
    *   **FileSystem (Workspace)**: `list_files`, `get_tree`, `read_file`, `write_file`, `append_text`, `search_files`, `replace_text`, `find_file`, `get_file_diff`, `undo_file_change`.
    *   **Intelligence (AST-Powered/Analysis)**: `find_usages`, `find_definitions`, `list_symbols`, `list_implementations`, `get_type_info`, `get_project_summary`, `get_file_skeleton`, `search_usages_globally`, `get_semantic_diff`, `rename_symbol`, `list_todos`, `go_doc`, `get_complexity_metrics`, `get_package_graph`, `generate_mermaid_diagram`, `move_definition`, `verify_architecture`, `get_code_health`, `get_detailed_coverage`, `analyze_sequence_flow`, `dead_code_graph`, `get_definitions`.
    *   **Git (Workspace)**: `get_git_status`, `get_git_diff`, `get_git_log`, `get_git_show`, `get_git_blame`, `git_commit`, `git_create_branch`.
    *   **Media & Vision (Integrations)**: `create_image` (Imagen 3), `read_image` (Vision).
    *   **State & Session**: `manage_scratchpad`, `manage_tasks`, `manage_config`, `get_session_info`, `summarize_history`, and `manage_history`.
        *   **Mode-Scoped Storage**: State files are now scoped to the configuration `MODE` (e.g., `output/vertex/tasks.json`, `output/vertex/scratchpad.md`, `output/vertex/config.json`) to prevent conflicts when switching environments.
        *   **Persistent Configuration**: `manage_config` allows storing key-value pairs (like webhook URLs) that persist across sessions for a specific mode.
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
        *   **Auto-Recovery**: The system automatically reconstructs missing cost ledgers by scanning session logs and backups.
*   **Vertex AI Optimized**: Native support for the official Google GenAI SDK, focused on Vertex AI for enterprise-grade security and performance.
*   **Automatic Token Management**: Automatically retrieves access tokens via `gcloud` with local caching for high performance.
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Safety Guardrails**: 
    *   **Automatic Maintenance (Self-Healing)**: Prevents "Context Overflow" by automatically summarizing older unpinned conversation turns when the payload exceeds 90% of `MAX_HISTORY_TOKENS`. This relieves pressure without losing the semantic core of the session.
    *   **Turn-Aligned Pinning**: Critical instructions and user-selected turns can be **pinned** (using `manage_history`) to protect them from pruning or summarization, ensuring they remain in the model's active context.
    *   **Clogged Context Detection**: If the history becomes "clogged" with pinned turns and maintenance cannot reduce the size below 85% of capacity, the agent injects an **URGENT SYSTEM NOTICE** instructing the model to persist state and stop before a hard rollback occurs.
    *   **Infinite Loop Protection**: 
        *   **SHA-256 Hashing**: Detects and breaks "Hallucination Loops" by hashing the model's full response (Thought + Text + Tools).
        *   **Repetition Guard**: Tracks identical tool calls with same arguments to prevent runaway execution cycles.
    *   **Internal Hard Budget**: Enforces a deterministic USD limit (Safe-by-Design) to halt sessions automatically if costs exceed safety thresholds.
    *   **Atlassian Reliability**: Centralized execution for Jira and Confluence tools featuring **exponential back-off** and automatic request body recovery for failed network calls.
    *   **Recursion Limit**: Prevents excessive turns using the `MAX_TURNS` configuration.
    *   **Descriptive Error Handling**: Automatically identifies and reports specific API block reasons (e.g., `SAFETY`, `RECITATION`) instead of generic "empty response" errors.
    *   **Command Safety**: `execute_command` includes a path-based validation gate. Commands that access files outside the working directory (e.g., `cat /etc/passwd`) require manual confirmation, even for whitelisted "safe" commands.
    *   **Secure Integrations**: Standardized URL construction and idiomatic error handling across Confluence, Jira, and Teams integrations to prevent injection and data leakage.
    *   **Persistent Authorization**: The `register_safepath` tool allows you to permanently authorize specific directories or files outside the project root. This requires a double-confirmation handshake for maximum security.
*   **Session Persistence & Archiving**: Automatically remembers conversation history. When starting a new session (`-new`), session-specific data (history and logs) is archived to `output/backups/`, while environment state (safepaths, tasks, and scratchpads) remains persistent.
    *   **Crash Resilience**: Automatically persists history after every turn (user prompt, model response, and tool results). If a system crash occurs during execution, a built-in **Auto-Repair** mechanism fixes the history on next startup to ensure the session remains valid and resumable.
*   **Bash Compatibility**: Uses identical file naming and structures as the original Bash project for full interoperability.

## 🛡️ Wallet Protection & Billing Transparency
`tell-me-go` is built to prevent the "hidden" cost spikes common in high-context AI development, specifically targeting **Vertex AI's tiered pricing model**.

*   **Price Cliff Guardrails**: 
    *   **The 128k Barrier**: Google bills sessions > 128k tokens at a **2x higher rate**. 
    *   **Safety Headroom**: The system is tuned with a default `MAX_HISTORY_TOKENS` of **120k**, providing a ~8k buffer for "Thinking" and response tokens.
    *   **Traffic-Light UI**: The terminal context indicator (**T**) turns **Yellow** at 100k and **Red** at 128k, signaling that the next turn will trigger the expensive tier.
*   **Deterministic Budgeting**: An internal `HardBudgetLimit` (USD) can be set programmatically to terminate sessions immediately if they exceed a pre-defined safety threshold.
*   **Cache Efficiency Indicators**: 
    *   **Exposure Tracking (%)**: The UI shows exactly what percentage of each turn is billed (**N**).
    *   **Green Metrics**: A **Green** percentage (<20%) indicates that Google's Context Cache is successfully serving 80%+ of your turn for free.
*   **Comprehensive Cost Auditing**:
    *   **Reasoning Transparency**: "Thinking" tokens (from GPT-5.2, DeepSeek-R1, and Claude) are tracked at high-cost output rates. The system explicitly maps these to the domain `llm.Part` and bills them accurately in its estimates.
    *   **Grounding Fees**: Flat-rate fees for Google Search queries ($0.035/ea) are factored into the total session cost.
    *   **Persistent Ledger**: All session costs are recorded in a centralized `global_costs.json` file for daily expenditure tracking via `get_cost_summary`.

## 📋 Prerequisites
*   **Go**: 1.25 or higher.
*   **Google Cloud SDK (gcloud)**: Required for Vertex AI authentication.

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
Run the assistant by passing your prompt as an argument. By default, it uses `configs/vertex.yaml`.

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
*   **Payload**: Estimated payload size / Max history tokens. Turns **YELLOW** at 100k and **RED** if the limit is exceeded.

**Post-turn Usage Metrics:**
After the model responds, a detailed breakdown is provided:
```text
[08:21:09] M: 31882 H: 1204 C: 402 Th: 0 [3.20s / 4.50s]
╰─⠿ Ready ($0.0123 $0.0542 $1.4745 $2.1050 M: 31882 H: 1204 3.6% O: 402)
```
*   **M (Misses)**: Paid tokens (Input minus Hits). Turns **WHITE** if misses > hits.
*   **H (Hits)**: Tokens served for free from Google's cache. Turns **GRAY** when active.
*   **C (Completion)**: Response tokens generated by the model.
*   **Th (Thinking)**: Thinking tokens used.
*   **[Time]**: `Model Latency / Total Turn Time`.
*   **($... $... $... $...)**: `Turn / Task / Session / Daily` costs. The **Session Cost** is highlighted in green.
*   **M / H / %**: Aggregate Session Misses, Hits, and Hit Rate.
*   **O (Output)**: Aggregate Session Output tokens.
*   **Payload (T)**: The key **Price Cliff** metric is displayed as a "Payload" line above the footer.
    *   **Logic**: This is the total context sent to the model. It determines if you are charged the **Standard** rate or the **2x Penalty** rate.
    *   **Gray**: < 100k (Standard billing).
    *   **Yellow**: 100k - 128k (Warning/Headroom).
    *   **Red**: > 128k (**High-Tier Pricing triggered**).

**Thinking Mode (Gemini 3):**
If `THINKING_BUDGET` or `THINKING_LEVEL` is set in your config, the assistant will display its reasoning process in the terminal.

**New Session:**
```bash
./tell-me-go -new "Start a fresh conversation."
```

## 📂 Shared Storage & Compatibility
`tell-me-go` uses a structured output directory for session history and state.

### Environment Variables
*   `TELL_ME_HOME`: The base directory for shared data (defaults to the current directory).
*   `AIT_HOME`: Fallback for compatibility with the original Bash project.

### Mode-Scoped State
Tasks and Scratchpads are scoped to the active `MODE` defined in your configuration file.
*   **Tasks**: stored in `output/<MODE>/tasks.json`
*   **Scratchpad**: stored in `output/<MODE>/scratchpad.md`

This allows you to maintain separate contexts (e.g., "Personal" vs "Work") simply by switching config files.

## ⚙️ Configuration
The tool is optimized for Google Vertex AI.

```yaml
# configs/vertex.yaml
MODE: "vertex"
PERSON: "A helpful AI assistant using Google Vertex AI."
AIMODEL: "gemini-3-flash-preview"
AIURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT_ID/locations/us-central1/publishers/google/models"

# --- Tools & Features ---
USE_SEARCH: true
THINKING_BUDGET: 0 # Max for gemini-3-flash-preview is 32768
THINKING_LEVEL: "HIGH" # Options: LOW, MEDIUM, HIGH
SHOW_THOUGHTS: true
SHOW_TOOLS: true

# --- Concurrent Execution ---
MAX_CONCURRENT_TOOLS: 5   # Maximum number of tools to execute in parallel
TOOL_TIMEOUT: 30          # Maximum duration (seconds) for any single tool call

# --- Safety & History ---
MAX_TURNS: 10              # Maximum tool calls per prompt (Recursion limit)
MAX_HISTORY_TURNS: 20      # Number of turns to keep in history file (Pruning)
MAX_HISTORY_TOKENS: 120000 # Max payload size before safety rollback (Headroom for 128k price cliff)

# --- Authentication ---
KEY_FILE: "" # Optional: Path to Service Account JSON key.

# --- Model Specific Overrides ---
MODELS:
  gemini-3-flash-preview:
    MAX_THINKING_BUDGET: 32768
    CONTEXT_WINDOW: 1048576
  gemini-3-pro-preview:
    MAX_THINKING_BUDGET: 65536
    CONTEXT_WINDOW: 2097152
```

### 🧠 Model-Specific Overrides
The `MODELS` section allows you to define hard technical limits for different model generations.
*   **`MAX_THINKING_BUDGET`**: Automatically clamps the `THINKING_BUDGET` to the model's supported maximum to prevent API errors.
*   **`CONTEXT_WINDOW`**: Provides the baseline for the agent's payload tracking and safety rollback logic.

## ⌨️ Shell Integration (Recommended)
To streamline your workflow, add these aliases to your `.bashrc` or `.zshrc`. This provides a fast, one-letter command (`b`) for interacting with the assistant and simplifies session management.

```bash
# 1. Set the home directory for session history, tasks, and scratchpad
export TELL_ME_HOME="$HOME/tell-me-go"

# 2. Main command alias (points to your preferred config)
# Using a function allows passing flags and prompts easily
b() {
    tell-me-go -c "$TELL_ME_HOME/configs/vertex.yaml" "$@"
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

## 🏗️ Design Decisions
Critical architectural decisions are documented as [ADRs](./docs/adr/) following our [ADR SOP](./docs/sop/standards/adr_standards.md). For detailed implementation plans, see the [Multi-Provider Technical Specification](./docs/sop/technical/multi_provider_implementation.md).

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)**. All development must follow the protocols defined in the [docs/sop/](./docs/sop/) directory.

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
