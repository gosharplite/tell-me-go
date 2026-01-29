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
    *   **FileSystem**: `list_files`, `get_tree`, `read_file`, `write_file`, `search_files`, `replace_text`, `find_file`, `grep_definitions`, `get_file_skeleton`, `get_file_diff`.
    *   **Intelligence (AST-Powered)**: `find_usages`, `list_implementations`, `get_type_info`, `get_project_summary`, `search_usages_globally`, `semantic_diff`, `rename_symbol`, `list_todos`, `go_doc`, `analyze_complexity`, `get_package_graph`.
    *   **Git**: `get_git_status`, `get_git_diff`, `get_git_log`, `get_git_commit`, `get_git_blame`.
    *   **Media & Vision**: `create_image` (Imagen 3), `read_image` (Vision).
    *   **State & Session**: `manage_scratchpad`, `manage_tasks`, `manage_config`, and `configure_ux_preferences`.
        *   **Mode-Scoped Storage**: State files are now scoped to the configuration `MODE` (e.g., `vertex_tasks.json`, `vertex_scratchpad.md`, `vertex_config.json`) to prevent conflicts when switching environments.
        *   **Persistent Configuration**: `manage_config` allows storing key-value pairs (like webhook URLs) that persist across sessions for a specific mode.
        *   **Smart Suggestions**: `configure_ux_preferences` enables a "Smart Suggestions" UI where the AI intelligently suggests 2-3 context-aware follow-up commands at the end of every response. This state is persistent and automatically injected into the AI's System Prompt.
        *   **Rollback**: `rollback_last_turn` allows undoing the last interaction.
    *   **External Integration**:
        *   **Teams**: `send_teams_message` sends rich Adaptive Cards to Microsoft Teams channels via Power Automate webhooks.
    *   **System**: `execute_command`, `ask_user`, `read_url`, `read_external_docs`, `http_request`, `register_safepath`, `list_safepaths`, `remove_safepath`.
    *   **Dev Tools**: `run_tests`, `go_tidy`, `get_coverage`, `run_linter`, `run_benchmark`, `check_vulnerabilities`.
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
    *   **Recursion Limit**: Prevents infinite tool-calling loops using the `MAX_TURNS` configuration.
    *   **Descriptive Error Handling**: Automatically identifies and reports specific API block reasons (e.g., `SAFETY`, `RECITATION`) instead of generic "empty response" errors.
    *   **Command Safety**: `execute_command` includes a path-based validation gate. Commands that access files outside the working directory (e.g., `cat /etc/passwd`) require manual confirmation, even for whitelisted "safe" commands.
    *   **Persistent Authorization**: The `register_safepath` tool allows you to permanently authorize specific directories or files outside the project root. This requires a double-confirmation handshake for maximum security.
*   **Session Persistence & Archiving**: Automatically remembers conversation history. When starting a new session (`-new`), session-specific data (history and logs) is archived to `output/backups/`, while environment state (safepaths, tasks, and scratchpads) remains persistent.
    *   **Crash Resilience**: Automatically persists history after every turn (user prompt, model response, and tool results). If a system crash occurs during execution, a built-in **Auto-Repair** mechanism fixes the history on next startup to ensure the session remains valid and resumable.
*   **Bash Compatibility**: Uses identical file naming and structures as the original Bash project for full interoperability.

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

**Raw Output Mode:**
You can also use the `-r` flag with a regular prompt to receive raw text without formatting:
```bash
./tell-me-go -r "Write a bash script to list files"
```

**Pre-flight Status Log:**
Before every request, the tool shows your current resource usage relative to configured limits:
`[14:19:43] [System (3/20)] Payload: ~4549/120000 tokens`
*   **(3/20)**: Current turn count / Max history turns.
*   **Tokens**: Estimated payload size / Max history tokens. Turns **RED** if >90% of `MAX_HISTORY_TOKENS`.

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
*   **Tasks**: stored in `output/<MODE>_tasks.json`
*   **Scratchpad**: stored in `output/<MODE>_scratchpad.md`

This allows you to maintain separate contexts (e.g., "Personal" vs "Work") simply by switching config files.

## ⚙️ Configuration
The tool is optimized for Google Vertex AI.

```yaml
# configs/vertex.yaml
MODE: "vertex"
PERSON: "A helpful AI assistant using Google Vertex AI."
AIMODEL: "gemini-2.0-flash-001"
AIURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT_ID/locations/us-central1/publishers/google/models"
USE_SEARCH: true
THINKING_BUDGET: 0
THINKING_LEVEL: ""  # Options: LOW, MEDIUM, HIGH

# --- Safety & History ---
MAX_TURNS: 10              # Maximum tool calls per prompt (Recursion limit)
MAX_HISTORY_TURNS: 20      # Number of turns to keep in history file (Pruning)
MAX_HISTORY_TOKENS: 120000 # Max payload size before safety rollback

# --- UI & Execution ---
SHOW_THOUGHTS: true        # Show model reasoning
SHOW_TOOLS: true          # Show tool execution status logs
MAX_CONCURRENT_TOOLS: 5    # Parallel tool execution limit
TOOL_TIMEOUT: 30           # Individual tool timeout (seconds)
```

### 🧠 Strategic Memory Management
To optimize for **cost efficiency** and **Gemini Context Caching**, the assistant uses a tiered memory strategy based on three critical variables:

1.  **`MAX_HISTORY_TOKENS` (Default: 120,000)**:
    *   **The Price Cliff**: Gemini 1.5/2.0 pricing tiers jump at **128k tokens**. Staying below 120k ensures you always pay the "Standard" rate ($0.025 - $0.31/1M) and avoid the "Premium" rate ($0.075 - $1.25/1M).
    *   **Safety Net**: If a response or tool output pushes the payload over this limit, the agent automatically **rolls back** the last turn to prevent a session crash or accidental high charges.

2.  **`MAX_HISTORY_TURNS`**:
    *   **50% Pruning Strategy**: When this limit is hit, the agent prunes the oldest **50%** of the conversation.
    *   **Why 50%?**: Gemini charges for the full "Cache Miss" when the prefix changes. By pruning half the history instead of just 1 turn, the agent creates a **stable cache prefix**. The next dozens of turns will be processed using high-speed, 90%-discounted cached tokens.
    *   **AI Re-Sync**: After a major prune, the agent injects an **Urgent System Notice** into the history, instructing the model to use the **Scratchpad** to recover its situational awareness.

3.  **`MAX_TURNS` (Default: 10)**:
    *   **Execution Safety**: Limits how many consecutive tool-calls the agent can make in a single prompt. This prevents infinite loops and controls turn-by-turn costs.

#### Recommended Settings:
| Model Type | MAX_HISTORY_TOKENS | MAX_HISTORY_TURNS | MAX_TURNS |
| :--- | :--- | :--- | :--- |
| **Flash (Fast/Light)** | 120,000 | **40** | 50 |
| **Pro (Deep/Complex)** | 120,000 | **20** | 50 |

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
