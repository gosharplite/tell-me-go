# tell-me-go: A Go Gemini CLI Assistant

A lightweight, terminal-based interface for Google's Gemini models, ported from the original [tell-me](https://github.com/gosharplite/tell-me) Bash assistant.

## Overview
`tell-me-go` is a high-performance, type-safe port of the `tell-me` tool. It provides a terminal-based interface to interact with Google's Gemini models (AI Studio and Vertex AI). By moving from Bash to Go, this project aims for better performance, easier distribution via single binaries, and a robust codebase governed by strict Standard Operating Procedures (SOPs).

## 🚀 Features
*   **Single Binary**: No dependency on `jq`, `yq`, or `curl`.
*   **Gemini AI Studio Support**: Direct integration with the Gemini 2.0 and 1.5 models.
*   **YAML Configuration**: Flexible setup via standard configuration files.
*   **SOP-Driven**: Built with a "Safety First" architecture where every change follows documented procedures.

## 📋 Prerequisites
*   **Go**: 1.21 or higher.
*   **Google AI Studio API Key**: Required for interaction with the model.

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
Run the assistant by passing your prompt as an argument:
```bash
./tell-me-go "What is the capital of France?"
```
*Note: Ensure your `API_KEY` is set in the environment or the config file.*

## ⚙️ Configuration
The tool looks for configuration in `configs/gemini.yaml`. You can provide your API key via an environment variable:
```bash
export API_KEY="your_api_key_here"
```

## 📜 SOP-Driven Development
This project adheres to strict **Standard Operating Procedures (SOPs)** to ensure stability and security. All development must follow the protocols defined in the [SOP/](./SOP/) directory, including:
*   [Git Workflow](./SOP/core/git_workflow.md)
*   [Public Release Process](./SOP/core/public_release.md)
*   [Testing Standards](./SOP/core/testing_standards.md)
*   [Project Architecture](./SOP/core/architecture_and_packages.md)

## ⚖️ License
This project is licensed under the [MIT License](LICENSE).

