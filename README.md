# tell-me-go: A Go Gemini CLI Assistant

A lightweight, terminal-based interface for Google's Gemini models, powered by the official Google GenAI SDK.

## Overview
`tell-me-go` is a high-performance, type-safe assistant designed for developers. It provides a terminal-based interface to interact with Google's Gemini models, specifically optimized for **Gemini 2.0+** reasoning and multimodal capabilities. Governed by strict Standard Operating Procedures (SOPs), it ensures a robust and predictable experience.

## 🚀 Features
*   **Official SDK**: Built on `google.golang.org/genai` for native support of the latest Gemini features.
*   **Gemini 2.0 Reasoning**: Support for **Thinking (Reasoning)** models with visible thought processes and configurable token budgets.
*   **Agentic Tools**: Natively executes local tools (e.g., `execute_command`, `read_url`, `list_files`, `read_file`) and Google Search to solve complex tasks.
*   **Shared State**: Shared Task Manager and Scratchpad across sessions and versions.
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Safety Guardrails**: 
    *   **Automatic Rollback**: Prevents "Context Overflow" by checking the estimated payload size against `MAX_HISTORY_TOKENS`. If exceeded, the history is restored to the previous turn.
    *   **Recursion Limit**: Prevents infinite tool-calling loops using the `MAX_TURNS` configuration.
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
PERSON: "A helpful AI assistant."
AIMODEL: "gemini-2.0-flash-001"
AIURL: "https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT_ID/locations/us-central1/publishers/google/models"
USE_SEARCH: true
THINKING_BUDGET: 1024
THINKING_LEVEL: "MEDIUM"  # Options: LOW, MEDIUM, HIGH

# --- Safety & History ---
MAX_TURNS: 10              # Maximum tool calls per prompt (Recursion limit)
MAX_HISTORY_TURNS: 20      # Number of turns to keep in history file (Pruning)
MAX_HISTORY_TOKENS: 120000 # Max payload size before safety rollback
```

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)**. All development must follow the protocols defined in the [SOP/](./SOP/) directory.

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).
