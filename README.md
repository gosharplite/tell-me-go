# tell-me-go: A Go Gemini CLI Assistant

A lightweight, terminal-based interface for Google's Gemini models, powered by the official Google GenAI SDK.

## Overview
`tell-me-go` is a high-performance, type-safe assistant designed for developers. It provides a terminal-based interface to interact with Google's Gemini models, specifically optimized for **Gemini 2.0+** reasoning and multimodal capabilities. Governed by strict Standard Operating Procedures (SOPs), it ensures a robust and predictable experience.

## 🚀 Features
*   **Official SDK**: Built on `google.golang.org/genai` for native support of the latest Gemini features.
*   **Gemini 2.0 Reasoning**: Support for **Thinking (Reasoning)** models with visible thought processes and configurable token budgets.
*   **Agentic Tools**: Natively executes local tools (e.g., `list_files`, `read_file`) and Google Search to solve complex tasks.
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Session Persistence**: Automatically remembers conversation history across CLI calls with valid role alternation.
*   **Vertex AI & Gemini API**: Seamlessly switches between backends based on configuration.
*   **Automatic Token Management**: Automatically retrieves access tokens via `gcloud` for Vertex AI sessions.
*   **SOP-Driven**: Built with a "Safety First" architecture where every change follows documented procedures.

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
3.  **Install (Optional)**:
    ```bash
    go install ./cmd/tell-me-go/
    ```

## 💻 Usage
Run the assistant by passing your prompt as an argument. By default, it uses `configs/vertex.yaml`.

**Basic Usage:**
```bash
./tell-me-go "What is the capital of France?"
```

**Thinking Mode (Gemini 2.0):**
If `THINKING_BUDGET` is set in your config, the assistant will display its reasoning process in the terminal.

**Custom Configuration:**
```bash
./tell-me-go -c configs/custom-vertex.yaml "Hello"
```

**New Session:**
```bash
./tell-me-go -new "Start a fresh conversation."
```

**Interactive Multi-line Mode:**
If no prompt is provided as an argument, the tool reads from `stdin`. Press `Ctrl+D` to send.

## ⌨️ Shell Integration (`a`)
To use the `a` shortcut, add the following to your `~/.bashrc` or `~/.zshrc`:

```bash
# Path to your tell-me-go binary
alias a='/path/to/tell-me-go'
```

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
MAX_TURNS: 10
```

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)**. All development must follow the protocols defined in the [SOP/](./SOP/) directory, including:
*   [Agentic Capabilities](./SOP/core/agentic_capabilities.md) (Function Calling)
*   [Project Architecture](./SOP/core/architecture_and_packages.md)
*   [History Management](./SOP/core/history_management.md)
*   [Authentication & Tokens](./SOP/core/authentication_and_token_management.md)
*   [Testing Standards](./SOP/core/testing_standards.md)

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).

