<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->


# Standard Operating Procedure (SOP): Documentation Standards

### Objective
To ensure that all documentation in the `tell-me-go` project—including the README, SOPs, and inline code comments—is clear, consistent, professional, and useful for both users and developers.

---

### Prerequisites
- Knowledge of Markdown syntax.
- Understanding of Go documentation conventions (Go doc).

---

### Step-by-Step Instructions

#### 1. README.md Structure
Every project repository must have a `README.md` in the root that follows this standard structure:

1.  **Project Title**: Large heading with a brief, one-sentence tagline.
2.  **Overview**: A concise description of the project's purpose and its relationship to the original `tell-me` (Bash).
3.  **🚀 Features**: A bulleted list of current capabilities.
4.  **📋 Prerequisites**: Tools and versions required (e.g., Go 1.25+).
5.  **🛠️ Installation**: Clear commands to build or install the tool.
6.  **💻 Usage**: Practical examples of CLI commands and expected output.
7.  **⚙️ Configuration**: Instructions on setting up YAML files and Environment Variables (e.g., `API_KEY`).
8.  **📜 SOP-Driven Development**: A note explaining that the project follows strict SOPs (linking to the `docs/sop/` directory).
9.  **⚖️ License**: Reference to the MIT license.

#### 2. Inline Code Documentation
- **Package Comments**: Every package must have a comment at the top explaining its responsibility.
- **Exported Members**: All exported functions, structs, and variables **must** have a comment.
- **Style**: Comments should be full sentences starting with the name of the member being documented.
    - *Example*: `// Load reads the configuration file from the disk.`

#### 3. SOP Documentation
SOPs must follow the structure defined in [SOP Management](../lifecycle/sop_management.md):
- Title, Objective, Prerequisites, Step-by-Step, Templates, Verification, and Best Practices.

#### 4. Maintenance and Updates
- **Sync**: Documentation must be updated in the same commit as the feature it describes.
- **Verification**: Check for broken links (relative paths) and formatting errors before committing.
- **README Priority**: The `README.md` is the primary entry point for users. It **must** be verified as up-to-date (reflecting all current tools, flags, and config options) before any public release.
- **Maintenance Loop**: Follow the [Documentation Maintenance and Verification](./documentation_maintenance.md) SOP for periodic audits and codebase synchronization.
- **Config Sync**: The example configurations in `README.md` must be kept in strict sync with the actual files in the `configs/` directory. Any new YAML keys or logic changes must be reflected in both places.
- **Review**: During a [Public Release](../lifecycle/public_release.md), a full documentation audit is mandatory.

---

### Code Templates

#### Go Doc Example:
```go
// Package history manages conversation persistence.
package history

// Manager handles reading and writing history files.
type Manager struct {
    // ...
}

// NewManager initializes a Manager with a specific file path.
func NewManager(path string) *Manager {
    return &Manager{Path: path}
}
```

---

### Verification
1.  **Format Check**: Ensure Markdown renders correctly in a previewer.
2.  **Go Doc Check**: Run `go doc ./...` to ensure all exported members are documented.
3.  **Completeness**: Verify that all new CLI flags or configuration keys are listed in the README.

---

### Best Practices
- **Conciseness**: Avoid fluff. Be technical and direct.
- **Visuals**: Use code blocks and ASCII/Markdown tables for clarity.
- **Relative Links**: Always use relative links to other files within the repository (e.g., `[Git Workflow](./git_workflow.md)`).
- **No Secrets**: Never include real API keys or sensitive data in examples.
- **State Management Consistency**: When documenting or providing examples for SOPs, always include the "Task Initialization" and "State Verification" steps required by [CLI Standards](./cli_standards.md).
