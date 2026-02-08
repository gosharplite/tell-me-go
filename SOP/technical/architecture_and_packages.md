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
    - `internal/agent`: The high-level orchestration engine.
    - `internal/cli`: Flat CLI layer, flag parsing, and command orchestration. Handles the **Mode-Scoped Storage** logic.
    - `internal/config`: Global configuration and YAML parsing.
    - `internal/domain`: Pure interfaces and entities (LLM, Tools, Pricing, Security).
    - `internal/infrastructure`: All external adapters and concrete implementations:
        - `internal/infrastructure/llm`: Gemini/Vertex AI adapter.
        - `internal/infrastructure/storage`: Filesystem utilities and asset storage.
        - `internal/infrastructure/security`: Security Manager and path guardrails.
        - `internal/infrastructure/auth`: Token management for Vertex AI.
        - `internal/infrastructure/history`: Session management and persistence.
        - `internal/infrastructure/pricing`: Cost calculation logic.
    - `internal/tools`: High-level agent capabilities (Analysis, Workspace, Media).
    - `internal/ui`: Flat UI rendering and interaction layer.
- **`configs/`**: Storage for default configuration templates (YAML).
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
- **Avoid Globals**: Do not use global variables for state. Pass dependencies explicitly via constructors.

---

### Directory Map

```text
tell-me-go/
├── cmd/
│   └── tell-me-go/
│       └── main.go       # Orchestration and Entry Point
├── internal/
│   ├── agent/            # Orchestration Engine
│   ├── cli/              # Flat CLI Layer
│   ├── config/           # Configuration
│   ├── domain/           # Core Domain Models & Interfaces
│   ├── infrastructure/   # External Adapters
│   │   ├── auth/         # Token Management
│   │   ├── history/      # Persistence
│   │   ├── llm/          # Gemini Adapter
│   │   ├── pricing/      # Cost Calculation
│   │   ├── security/     # Security Guardrails
│   │   └── storage/      # FS Utilities & Assets
│   ├── tools/            # Agent Capabilities
│   └── ui/               # Flat UI Layer
├── configs/              # Templates
├── tests/                # E2E Tests
├── SOP/                  # Documentation
├── go.mod
└── go.sum
```

#### 4. Mode-Scoped Storage
To prevent context leakage between different environments, the application implements **Mode-Scoped Storage**. 

- **Mechanism**: All session data, persistent state, and logs are stored within subdirectories under `output/` named after the `MODE` variable.
- **Paths Managed**:
    - `output/<MODE>/history.json`: Conversation history.
    - `output/<MODE>/config.json`: Persistent key-value settings.
    - `output/<MODE>/safepaths.json`: Authorized directory permissions.
    - `output/<MODE>/tasks.json`: Persistent task list.
    - `output/<MODE>/scratchpad.md`: Working memory.

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
1.  **Circular Dependency Check**: Run `go test ./internal/tools/analysis/...` to verify architecture.
2.  **Architecture Alignment**: Before committing, verify that new files follow the directory map above.
3.  **Encapsulation**: Check that `internal/` code is not being leaked.

---

### Best Practices
- **Flat is Better**: Avoid deep nesting.
- **Main is for Wiring**: Use `main.go` only to instantiate dependencies.
- **Context Propagation**: Always propagate `context.Context`.
- **Standard Library First**: Prefer Go's standard library over external dependencies.
