# Standard Operating Procedure (SOP): Project Architecture and Package Standards

### Objective
To define a consistent, scalable, and idiomatic Go directory structure for `tell-me-go`, ensuring clear separation of concerns, easy testing, and prevention of circular dependencies.

---

### Prerequisites
- Go toolchain 1.21+.
- Familiarity with the [Standard Go Project Layout](https://github.com/golang-standards/project-layout).

---

### Step-by-Step Instructions

#### 1. Directory Categorization
The project is organized into the following top-level directories:

- **`cmd/`**: Contains the main entry points for the application. Each subdirectory should match the binary name (e.g., `cmd/tell-me-go/`).
    - *Constraint*: Keep logic in `main.go` minimal. It should only handle initialization and hand off to `internal/`. **Business logic, specifically loops and orchestration, MUST NOT reside in `main`**; it must be moved to an internal package to ensure it is unit-testable.
- **`internal/`**: Contains private application and library code.
    - `internal/agent`: The high-level orchestration loop (Agent/Orchestrator).
    - `internal/config`: Configuration loading and YAML parsing.
    - `internal/api`: Communication logic for Gemini (AI Studio/Vertex).
    - `internal/history`: Session management and JSON persistence.
    - `internal/cli`: Terminal UI, flag parsing, and command orchestration.
- **`pkg/`**: Contains library code that is safe for use by external applications. 
- **`configs/`**: Storage for default configuration templates (YAML).
- **`SOP/`**: Project governance and process documentation.

#### 2. Package Naming and Visibility
- **Naming**: Use short, lowercase, single-word names (e.g., `config`, not `config_loader`).
- **Visibility**: Keep structs and functions unexported (lowercase) by default. Export (Uppercase) only what is strictly necessary for cross-package interaction.
- **Internal vs Pkg**: Always start new logic in `internal/`. Only move to `pkg/` if there is a demonstrated need for external reuse.

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
│   ├── api/              # Gemini Client Logic
│   ├── config/           # YAML/Env Loading
│   ├── history/          # Conversation Storage
│   └── terminal/         # UI and Markdown Rendering
├── pkg/
│   └── utils/            # General purpose helpers
├── configs/
│   ├── gemini.yaml       # AI Studio Template
│   └── vertex.yaml       # Vertex AI Template
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
	APIKey string `yaml:"API_KEY"`
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
- **Standard Library First**: Prefer Go's standard library over external dependencies (e.g., `encoding/json` over `easyjson`).

