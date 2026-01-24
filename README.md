# tell-me-go: A Go Gemini CLI Assistant

A lightweight, terminal-based interface for Google's Gemini models, ported from the original [tell-me](https://github.com/gosharplite/tell-me) Bash assistant.

## Overview
`tell-me-go` is a high-performance, type-safe port of the `tell-me` tool. It provides a terminal-based interface to interact with Google's Gemini models. By moving from Bash to Go, this project aims for better performance, easier distribution via single binaries, and a robust codebase governed by strict Standard Operating Procedures (SOPs).

## 🚀 Features
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Dual Auth Support**: Supports both **Google AI Studio** (API Key) and **Google Vertex AI** (GCP OAuth2 Tokens).
*   **Automatic Token Management**: Automatically retrieves access tokens via `gcloud` for Vertex AI sessions.
*   **Flexible Configuration**: Specify custom YAML configuration files for different models and environments.
*   **SOP-Driven**: Built with a "Safety First" architecture where every change follows documented procedures.

## 📋 Prerequisites
*   **Go**: 1.21 or higher.
*   **Google AI Studio API Key**: (Optional) For AI Studio mode.
*   **Google Cloud SDK (gcloud)**: (Optional) For Vertex AI mode using user credentials.

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
Run the assistant by passing your prompt as an argument. By default, it uses `configs/gemini.yaml`.

**Basic Usage (AI Studio):**
```bash
./tell-me-go "What is the capital of France?"
```

**Custom Configuration (Vertex AI):**
```bash
./tell-me-go -c configs/vertex.yaml "What is the capital of France?"
```

## ⚙️ Configuration
The tool supports two authentication methods based on your YAML configuration:

### 1. Google AI Studio (API Key)
Ensure your `AIURL` points to `generativelanguage.googleapis.com` and provide an `API_KEY`.
```bash
export API_KEY="your_api_key_here"
./tell-me-go "Hello"
```

### 2. Google Vertex AI (Bearer Token)
Ensure your `AIURL` points to `aiplatform.googleapis.com`. The tool will automatically use `gcloud auth print-access-token` to authenticate.
```bash
./tell-me-go -c configs/vertex.yaml "Hello"
```

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)** to ensure stability and security. All development must follow the protocols defined in the [SOP/](./SOP/) directory, including:
*   [Git Workflow](./SOP/core/git_workflow.md)
*   [Public Release Process](./SOP/core/public_release.md)
*   [Testing Standards](./SOP/core/testing_standards.md)
*   [Project Architecture](./SOP/core/architecture_and_packages.md)
*   [Authentication & Tokens](./SOP/core/authentication_and_token_management.md)
*   [CLI Standards](./SOP/core/cli_standards.md)
*   [Documentation Standards](./SOP/core/documentation_standards.md)

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).

