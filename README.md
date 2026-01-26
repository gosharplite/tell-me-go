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
    *   **Interactive Safety**: Serialized terminal prompts for tool confirmations—no more "flying by" messages.
    *   **FileSystem**: `list_files`, `get_tree`, `read_file`, `write_file`, `search_files`, `replace_text`, `find_file`, `grep_definitions`, `get_file_skeleton`, `get_file_diff`.
    *   **Intelligence (AST-Powered)**: `find_usages`, `list_implementations`, `get_type_info`, `get_project_summary`, `search_usages_globally`, `semantic_diff`, `rename_symbol`, `list_todos`, `go_doc`, `analyze_complexity`, `get_package_graph`.
    *   **Git**: `get_git_status`, `get_git_diff`, `get_git_log`, `get_git_commit`, `get_git_blame`.
    *   **Media & Vision**: `create_image` (Imagen 3), `read_image` (Vision).
    *   **State & Session**: `manage_scratchpad`, `manage_tasks`, `rollback_last_turn`.
    *   **System**: `execute_command`, `ask_user`, `read_url`, `read_external_docs`, `http_request`.
    *   **Dev Tools**: `run_tests`, `go_tidy`, `get_coverage`, `run_linter`, `run_benchmark`, `check_vulnerabilities`.
    *   **Financial Metrics (Dynamic Pricing)**: 
        *   `estimate_cost`: Provides a detailed session cost breakdown using live-synced Vertex AI rates.
        *   `get_cost_summary`: Generates a daily expenditure report from a local persistent ledger (`output/cost-history.json`).
        *   **Auto-Sync**: Automatically fetches the latest Google Cloud pricing from GitHub and caches it locally for 24 hours.
*   **Shared State**: Shared Task Manager and Scratchpad across sessions and versions.
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Safety Guardrails**: 
    *   **Automatic Rollback**: Prevents "Context Overflow" by checking the estimated payload size against `MAX_HISTORY_TOKENS`. If exceeded, the history is restored to the previous turn.
    *   **Recursion Limit**: Prevents infinite tool-calling loops using the `MAX_TURNS` configuration.
    *   **Command Safety**: `execute_command` includes a path-based validation gate. Commands that access files outside the working directory (e.g., `cat /etc/passwd`) require manual confirmation, even for whitelisted "safe" commands.
*   **Session Persistence & Archiving**: Automatically remembers conversation history. When starting a new session (`-new`), existing files are archived to `output/backups/`.
*   **Vertex AI & Gemini API**: Seamlessly switches between backends based on configuration.
*   **Automatic Token Management**: Automatically retrieves access tokens via `gcloud` with local caching for high performance.
*   **Bash Compatibility**: Uses identical file naming and structures as the original Bash project for full interoperability.

## 📋 Prerequisites
*   **Go**: 1.24 or higher.
*   **Google Cloud SDK (gcloud)**: Required for Vertex AI mode using user credentials.

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

**Pre-flight Payload Log:**
Before every request, the tool shows the estimated token count of the history and tools being sent:
`[14:19:43] [System] Payload: ~4549 tokens`

**Thinking Mode (Gemini 2.0):**
If `THINKING_BUDGET` is set in your config, the assistant will display its reasoning process in the terminal.

**New Session:**
```bash
./tell-me-go -new "Start a fresh conversation."
```

## 📂 Shared Storage & Compatibility
`tell-me-go` is designed to be fully compatible with the data structures of the original `tell-me` Bash project. 

### Environment Variables
*   `TELL_ME_HOME`: The base directory for shared data (defaults to the current directory).
*   `AIT_HOME`: Fallback for compatibility with the original Bash project.

## ⚙️ Configuration
The tool supports both Vertex AI and the Gemini Developer API.

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

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)**. All development must follow the protocols defined in the [SOP/](./SOP/) directory.

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
