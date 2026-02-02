<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# tell-me-go: A Go Gemini CLI Assistant

A lightweight, terminal-based interface for Google's Gemini models, powered by the official Google GenAI SDK.

## Overview
`tell-me-go` is a high-performance, type-safe assistant designed for developers. It provides a terminal-based interface to interact with Google's Gemini models, specifically optimized for **Gemini 2.0+** reasoning and multimodal capabilities. Governed by strict Standard Operating Procedures (SOPs), it ensures a robust and predictable experience.

## 🚀 Features
*   **Official SDK**: Built on `google.golang.org/genai` for native support of the latest Gemini features.
*   **Gemini Reasoning**: Support for **Thinking (Reasoning)** models with configurable budgets.
    *   **Smart Budget Cap**: Automatically adjusts `THINKING_BUDGET` to the maximum allowed by the selected model, preventing `Error 400` failures.
*   **Agentic Tools**: Natively executes a vast library of local tools and Google Search to solve complex tasks.
    *   **UI Controls**: Configurable visibility for thought processes (`SHOW_THOUGHTS`) and tool execution logs (`SHOW_TOOLS`) for a cleaner terminal experience.
    *   **Interactive Safety**: 
        *   **Serialized Prompts**: Tool headers are sequenced to prevent parallel execution logs from garbling interactive prompts.
        *   **Session-Persistent Bypass**: The `bypass_confirmation` tool state is now persistent for the entire session. No more re-authorizing every run.
    *   **FileSystem**: `list_files`, `get_tree`, `read_file`, `write_file`, `append_text`, `search_files`, `replace_text`, `find_file`, `grep_definitions`, `get_file_skeleton`, `get_file_diff`, `undo_file_change`.
    *   **Intelligence (AST-Powered)**: `find_usages`, `find_definitions`, `list_symbols`, `list_implementations`, `get_type_info`, `get_project_summary`, `search_usages_globally`, `semantic_diff`, `rename_symbol`, `list_todos`, `go_doc`, `analyze_complexity`, `get_package_graph`, `move_definition`.
    *   **Git**: `get_git_status`, `get_git_diff`, `get_git_log`, `get_git_commit`, `get_git_blame`, `git_commit`, `git_create_branch`.
    *   **Media & Vision**: `create_image` (Imagen 3), `read_image` (Vision).
    *   **State & Session**: `manage_scratchpad`, `manage_tasks`, `manage_config`, `get_session_info`, and `summarize_history`.
        *   **Mode-Scoped Storage**: State files are now scoped to the configuration `MODE` (e.g., `output/vertex/tasks.json`, `output/vertex/scratchpad.md`, `output/vertex/config.json`) to prevent conflicts when switching environments.
        *   **Persistent Configuration**: `manage_config` allows storing key-value pairs (like webhook URLs) that persist across sessions for a specific mode.
    *   **External Integration**:
        *   **Teams**: `send_teams_message` sends rich Adaptive Cards to Microsoft Teams channels via Power Automate webhooks.
    *   **System**: `execute_command`, `pipe_commands`, `ask_user`, `read_external_docs`, `http_request`, `register_safepath`, `list_safepaths`, `remove_safepath`, `register_readpath`, `list_readpaths`, `remove_readpath`, `bypass_confirmation`, `revoke_bypass`.
    *   **Dev Tools**: `run_tests`, `go_tidy`, `get_coverage`, `run_linter`, `run_benchmark`, `check_vulnerabilities`, `verify_release_readiness`.
    *   **Financial Metrics (Dynamic Pricing)**: 
        *   `estimate_cost`: Provides a detailed session cost breakdown using live-synced Vertex AI rates.
        *   `get_cost_summary`: Generates a daily expenditure report from a local persistent ledger (`output/global_costs.json`).
        *   **Auto-Sync**: Automatically fetches the latest Google Cloud pricing from GitHub and caches it locally for 24 hours.
*   **Vertex AI Optimized**: Native support for the official Google GenAI SDK, focused on Vertex AI for enterprise-grade security and performance.
*   **Automatic Token Management**: Automatically retrieves access tokens via `gcloud` with local caching for high performance.
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Safety Guardrails**: 
    *   **Automatic Rollback**: Prevents "Context Overflow" by checking the estimated payload size against `MAX_HISTORY_TOKENS`. If exceeded, the history is restored to the previous turn and an error is returned.
    *   **Pre-Limit Warnings**: 
        *   **AI Awareness**: When `MAX_TURNS` or `MAX_HISTORY_TOKENS` (90%+) is nearing, the agent injects escalating **System Notices** into the AI's volatile history. 
        *   **Graceful Exit**: This instructs the model to prioritize state persistence (scratchpad/tasks) before the process terminates or rolls back.
    *   **Infinite Loop Protection**: 
        *   **SHA-256 Hashing**: Detects and breaks "Hallucination Loops" by hashing the model's full response (Thought + Text + Tools).
        *   **Repetition Guard**: Tracks identical tool calls with same arguments to prevent runaway execution cycles.
    *   **Internal Hard Budget**: Enforces a deterministic USD limit (Safe-by-Design) to halt sessions automatically if costs exceed safety thresholds.
    *   **Recursion Limit**: Prevents excessive turns using the `MAX_TURNS` configuration.
    *   **Descriptive Error Handling**: Automatically identifies and reports specific API block reasons (e.g., `SAFETY`, `RECITATION`) instead of generic "empty response" errors.
    *   **Command Safety**: `execute_command` includes a path-based validation gate. Commands that access files outside the working directory (e.g., `cat /etc/passwd`) require manual confirmation, even for whitelisted "safe" commands.
    *   **Persistent Authorization**: The `register_safepath` tool allows you to permanently authorize specific directories or files outside the project root. This requires a double-confirmation handshake for maximum security.
*   **Session Persistence & Archiving**: Automatically remembers conversation history. When starting a new session (`-new`), session-specific data (history and logs) is archived to `output/backups/`, while environment state (safepaths, tasks, and scratchpads) remains persistent.
    *   **Crash Resilience**: Automatically persists history after every turn (user prompt, model response, and tool results). If a system crash occurs during execution, a built-in **Auto-Repair** mechanism fixes the history on next startup to ensure the session remains valid and resumable.
*   **Bash Compatibility**: Uses identical file naming and structures as the original Bash project for full interoperability.

## 🛡️ Wallet Protection & Billing Transparency
`tell-me-go` is built to prevent the "hidden" cost spikes common in high-context AI development, specifically targeting **Vertex AI's tiered pricing model**.

*   **Price Cliff Guardrails**: 
    *   **The 128k Barrier**: Google bills sessions > 128k tokens at a **2x higher rate**. 
    *   **Safety Headroom**: The system is tuned with a default `MAX_HISTORY_TOKENS` of **100k**, providing a ~28k buffer for "Thinking" and response tokens.
    *   **Traffic-Light UI**: The terminal context indicator (**T**) turns **Yellow** at 100k and **Red** at 128k, signaling that the next turn will trigger the expensive tier.
*   **Deterministic Budgeting**: An internal `HardBudgetLimit` (USD) can be set programmatically to terminate sessions immediately if they exceed a pre-defined safety threshold.
*   **Cache Efficiency Indicators**: 
    *   **Exposure Tracking (%)**: The UI shows exactly what percentage of each turn is billed (**N**).
    *   **Green Metrics**: A **Green** percentage (<20%) indicates that Google's Context Cache is successfully serving 80%+ of your turn for free.
*   **Comprehensive Cost Auditing**:
    *   **Reasoning Transparency**: "Thinking" tokens are high-cost (Output rate). The system explicitly tracks and bills them in its estimates.
    *   **Grounding Fees**: Flat-rate fees for Google Search queries ($0.035/ea) are factored into the total session cost.
    *   **Persistent Ledger**: All session costs are recorded in a centralized `global_costs.json` file for daily expenditure tracking via `get_cost_summary`.

## 📋 Prerequisites
*   **Go**: 1.24 or higher.
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
`[14:19:43] [System (3/20)] Payload: ~4549/100000 tokens`
*   **(3/20)**: Current turn count / Max history turns.
*   **Tokens**: Estimated payload size / Max history tokens. Turns **RED** if >90% of `MAX_HISTORY_TOKENS`.

**Post-turn Usage Metrics:**
After the model responds, a detailed breakdown is provided:
`[08:21:09] H: 1204 M: 31882 C: 402 T: 33488 N: 32284(96%) S: 1 Th: 0`
*   **H (Hits)**: Tokens served for free from Google's cache. Turns **GRAY** when active.
*   **M (Misses)**: Paid tokens (Input minus Hits). Turns **WHITE** if misses > hits.
*   **C (Completion)**: Response tokens generated by the model.
*   **T (Total Context)**: The key **Price Cliff** metric.
    *   **Logic**: This is the sum of `Input + Response`. It acts as the **Price Selector**: it determines if you are charged the **Standard** rate or the **2x Penalty** rate.
    *   **Gray**: < 100k (Standard billing).
    *   **Yellow**: 100k - 128k (Warning/Headroom).
    *   **Red**: > 128k (**High-Tier Pricing triggered** for the entire turn).
*   **N (%)**: **Billed Tokens**. The actual **Quantity** of tokens you are purchasing at the rate determined by **T**.
    *   **Green**: < 20% (Highly efficient caching).
    *   **Gray**: 20% - 70% (Standard).
    *   **White**: > 70% (Cache cold/High exposure).
*   **S/Th**: Number of Google Search queries and Thinking tokens used.

**Thinking Mode (Gemini 2.0):**
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
AIMODEL: "gemini-2.0-flash-001"
AIURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT_ID/locations/us-central1/publishers/google/models"

# --- Tools & Features ---
USE_SEARCH: true
THINKING_BUDGET: 0 # Max for gemini-2.5-flash is 24576
THINKING_LEVEL: "" # Options: LOW, MEDIUM, HIGH
SHOW_THOUGHTS: true
SHOW_TOOLS: true

# --- Concurrent Execution ---
MAX_CONCURRENT_TOOLS: 5   # Maximum number of tools to execute in parallel
TOOL_TIMEOUT: 30          # Maximum duration (seconds) for any single tool call

# --- Safety & History ---
MAX_TURNS: 10              # Maximum tool calls per prompt (Recursion limit)
MAX_HISTORY_TURNS: 20      # Number of turns to keep in history file (Pruning)
MAX_HISTORY_TOKENS: 100000 # Max payload size before safety rollback (Headroom for 128k price cliff)

# --- Authentication ---
KEY_FILE: "" # Optional: Path to Service Account JSON key.

# --- Model Specific Overrides ---
MODELS:
  gemini-2.0-flash:
    MAX_THINKING_BUDGET: 24576
    CONTEXT_WINDOW: 1048576
  gemini-1.5-pro:
    MAX_THINKING_BUDGET: 0
    CONTEXT_WINDOW: 2097152
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

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)**. All development must follow the protocols defined in the [SOP/](./SOP/) directory.

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
