<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->


# Standard Operating Procedure (SOP): Project Architecture and Package Standards

### Objective
To define a consistent, scalable, and idiomatic Go directory structure for `tell-me-go`, ensuring clear separation of concerns, easy testing, and prevention of circular dependencies.

---

### Prerequisites
- Go toolchain 1.24+.
- Familiarity with the [Standard Go Project Layout](https://github.com/golang-standards/project-layout).

---

### Step-by-Step Instructions

#### 1. Directory Categorization
The project is organized into the following top-level directories:

- **`cmd/`**: Contains the main entry points for the application. Each subdirectory should match the binary name (e.g., `cmd/tell-me-go/`).
    - *Constraint*: Keep `main.go` extremely minimal. It MUST ONLY initialize the `cli.App` and call `Run()`. All logic, specifically flag definitions, configuration loading, and component wiring, must reside in `internal/cli` to ensure the application lifecycle is programmatically testable.
- **`internal/`**: Contains private application and library code.
    - `internal/agent`: The high-level orchestration loop (Agent/Orchestrator).
    - `internal/config`: Configuration loading and YAML parsing.
    - `internal/api`: Communication logic using `google.golang.org/genai`.
    - `internal/history`: Session management and JSON persistence.
    - `internal/cli`: Terminal UI, flag parsing, and command orchestration.
    - `internal/auth`: Token management for Vertex AI.
    - `internal/tools`: Registry and implementation of executable functions.
- **`configs/`**: Storage for default configuration templates (YAML).
    - `configs/vertex.yaml`: The primary configuration template.
- **`SOP/`**: Project governance and process documentation.
- **`assets/`**: Static assets like pricing data.
- **`tests/`**: Integration and End-to-End tests.

#### 2. Package Naming and Visibility
- **Naming**: Use short, lowercase, single-word names (e.g., `config`, not `config_loader`).
- **Visibility**: Keep structs and functions unexported (lowercase) by default. Export (Uppercase) only what is strictly necessary for cross-package interaction.
- **Internal Only**: Always keep application logic in `internal/`. Do not create a `pkg/` directory unless there is a specific need for public library consumption.

#### 3. Dependency Management
- **Constructor Pattern**: Use `New<StructName>` functions to initialize packages (e.g., `func NewClient(apiKey string) *Client`).
- **Interfaces**: Define interfaces at the *consumer* side to enable easy mocking in tests.
- **Avoid Globals**: Do not use global variables for state (e.g., global API keys or history buffers). Pass dependencies explicitly via constructors.

---

### Directory Map

```text
tell-me-go/
├── cmd/
│   └── tell-me-go/
│       └── main.go       # Orchestration and Entry Point
├── internal/
│   ├── agent/            # High-level Agent Loop
│   ├── api/              # Gemini Client Logic
│   ├── auth/             # Token Management
│   ├── cli/              # CLI logic and Flag Parsing
│   ├── config/           # YAML/Env Loading
│   ├── history/          # Conversation Storage
│   └── tools/            # Agent Tools
├── configs/
│   └── vertex.yaml       # Vertex AI Template
├── tests/
│   └── e2e/              # End-to-End Tests
├── SOP/                  # Documentation
├── go.mod
└── go.sum
```

---

### Code Templates

#### New Internal Package Boilerplate:
```go
package config

// Config holds the application settings
type Config struct {
	Model  string `yaml:"AIMODEL"`
	Mode   string `yaml:"MODE"`
}

// New returns a fresh configuration instance
func New() *Config {
	return &Config{}
}
```

---

### Verification
1.  **Circular Dependency Check**: Ensure no package imports another that eventually imports it back. Run `go list -f '{{.ImportPath}} -> {{.Imports}}' ./...`.
2.  **Architecture Alignment**: Before committing, verify that new files follow the directory map above.
3.  **Encapsulation**: Check that `internal/` code is not being leaked to `pkg/`.

---

### Best Practices
- **Flat is Better**: Avoid deep nesting (e.g., `internal/api/gemini/v1/client`). Keep the hierarchy as flat as possible.
- **Main is for Wiring**: Use `main.go` only to instantiate dependencies and "wire" them together.
- **Context Propagation**: Always propagate `context.Context` from the entry point (`cli.Run`) down to all leaf operations (API calls, tool execution, DB/File I/O). Respect context cancellation to allow for graceful shutdown on user signals (SIGINT).
- **Standard Library First**: Prefer Go's standard library over external dependencies (e.g., `encoding/json` over `easyjson`).
