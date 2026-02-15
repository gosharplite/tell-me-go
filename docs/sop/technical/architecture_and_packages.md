<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->


# Standard Operating Procedure (SOP): Project Architecture and Package Standards

### Objective
To define a consistent, scalable, and idiomatic Go directory structure for `tell-me-go`, ensuring clear separation of concerns, easy testing, and prevention of circular dependencies.

---

### Prerequisites
- Go toolchain 1.25+.
- Familiarity with the [Standard Go Project Layout](https://github.com/golang-standards/project-layout).

---

### Step-by-Step Instructions

#### 1. Directory Categorization
The project is organized into the following top-level directories:

- **`cmd/`**: Contains the main entry points for the application. Each subdirectory should match the binary name (e.g., `cmd/tell-me-go/`).
    - *Constraint*: Keep `main.go` extremely minimal. It MUST ONLY initialize the `cli.App` and call `Run()`. All logic, specifically flag definitions, configuration loading, and component wiring, must reside in `internal/cli` to ensure the application lifecycle is programmatically testable.
- **`internal/`**: Contains private application and library code.
    - `internal/agent`: The high-level coordination layer.
        - `internal/agent/orchestration`: Multi-turn loop, state management, and turn-based logic.
        - `internal/agent/executor`: Tool execution and worker pool management.
    - `internal/cli`: Flat CLI layer, flag parsing, and command orchestration. Handles the **Mode-Scoped Storage** logic.
    - `internal/domain`: Pure interfaces and entities (LLM, Tools, Pricing, Security, Events).
    - `internal/infrastructure`: All external adapters and concrete implementations:
        - `internal/infrastructure/config`: Configuration loading and defaults.
        - `internal/infrastructure/auth`: Authentication and token management (e.g., Google/Vertex).
        - `internal/infrastructure/llm`: Language model provider adapters.
        - `internal/infrastructure/history`: Conversation history persistence.
        - `internal/infrastructure/storage`: File system and asset management.
        - `internal/infrastructure/security`: Sandbox, path guardrails, and confirmation gates.
        - `internal/infrastructure/telemetry`: Logging, metrics, and event tracking.
        - `internal/infrastructure/registry`: Tool and command registration.
        - `internal/infrastructure/exec`: OS-level command execution.
        - `internal/infrastructure/testing`: Shared mocks and test helpers.
    - `internal/tools`: Categorized agent capabilities:
        - `internal/tools/analysis`: AST-based code intelligence and semantic search.
        - `internal/tools/workspace`: Basic file system and project management.
        - `internal/tools/developer`: Testing, linting, and development workflow tools.
        - `internal/tools/integrations`: External service integrations (e.g., MS Teams).
    - `internal/ui`: Flat UI rendering and interaction layer.
- **`configs/`**: Storage for default configuration templates (YAML).
- **`docs/sop/`**: Project governance and process documentation.
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
│   ├── agent/            # Coordination Layer
│   │   ├── orchestration/ # Turn-based loop
│   │   └── executor/     # Tool execution
│   ├── cli/              # Flat CLI Layer
│   ├── domain/           # Core Domain Models & Interfaces
│   ├── infrastructure/   # External Adapters
│   │   ├── auth/         # Token Management
│   │   ├── config/       # Configuration
│   │   ├── history/      # Persistence
│   │   ├── llm/          # Gemini Adapter
│   │   ├── security/     # Security Guardrails
│   │   ├── storage/      # FS Utilities & Assets
│   │   ├── telemetry/    # Observability
│   │   └── testing/      # Shared Mocks
│   ├── tools/            # Agent Capabilities
│   │   ├── analysis/     # AST/Code Intelligence
│   │   ├── developer/    # Dev Workflow (Tests, Linters)
│   │   ├── integrations/ # External Services
│   │   └── workspace/    # File Operations
│   └── ui/               # Flat UI Layer
├── configs/              # Templates
├── tests/                # E2E Tests
├── docs/sop/                  # Documentation
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
