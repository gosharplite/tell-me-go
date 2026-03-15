<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Documentation Maintenance and Verification

### Objective
To ensure that all project documentation (README, SOPs, and References) remains a "Single Source of Truth" by maintaining 1:1 parity with the actual Go source code and project structure.

---

### Prerequisites
- Go toolchain 1.26+.
- Knowledge of [Documentation Standards](./documentation_standards.md).
- Access to Go static analysis tools (`go doc`, `go list`).

---

### Step-by-Step Instructions

#### 1. Map: Architectural Analysis
Before updating documentation, analyze the current state of the codebase to identify changes:
- **Package Graph**: Run `go list -f '{{.ImportPath}} -> {{.Imports}}' ./...` to identify new or removed packages.
- **Symbol Audit**: Use `go doc ./internal/<package>` to list all exported functions and types.
- **Tool Inventory**: For agentic tools, audit `internal/tools/` to find all calls to `Register()` and `RegisterWithOptions()`.

#### 2. Sync: Feature and API Alignment
Ensure every technical reference matches the implementation:
- **CLI Flags**: Verify that flags defined in `internal/cli/flags.go` or `main.go` are listed in `README.md`.
- **Configuration Keys**: Match the fields in the `internal/config` structs with the example YAML files in `configs/`.
- **Tool Specs**: Verify that the tool descriptions and parameters in `internal/tools/` match the feature lists in the documentation.
- **Mode-Scoped Storage**: Ensure any new persistent files follow the `<MODE>_filename` convention as defined in the Architecture SOP.

#### 3. Audit: Link and Path Integrity
Perform a repository-wide audit of all Markdown links:
- **Relative Paths**: Ensure all internal links (e.g., `[Link Text](./target.md)`) resolve correctly based on the file's location.
- **Cross-References**: Check that links between `docs/sop/` subdirectories (e.g., from `standards/` to `lifecycle/`) use the correct number of parent markers (`../`).
- **File Renames**: If a Go file or SOP is renamed, update all referring documentation immediately.

#### 4. Formatting and Compliance
- **SPDX Headers**: Verify that all new documentation files include the standard MIT license header.
- **Active Voice**: Use technical, active voice (e.g., "The agent executes the tool" instead of "The tool is executed by the agent").
- **Code Snippets**: Ensure Go code examples in SOPs are syntactically correct and follow the project's [Testing Standards](./testing_standards.md).

---

### Verification
1.  **Go Doc Check**: Run `go doc ./...` and verify that all exported members have clear, descriptive comments.
2.  **Manual Link Walkthrough**: Click through all relative links in the updated Markdown files to ensure they resolve.
3.  **Config Validation**: Compare `README.md` example configurations against `internal/config/defaults.go` for consistency.

---

### Best Practices
- **Doc-as-Code**: Treat documentation updates as a mandatory part of the "Definition of Done" for any feature.
- **No Manual Entry**: Where possible, use automated tools to generate API references from source code.
- **Context Preservation**: When documenting complex logic (like history pruning), explain the "Why" (e.g., "to enable context caching") to help future maintainers.
