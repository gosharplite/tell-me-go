<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Creating and Managing SOPs

### Objective
This SOP defines the standard structure and process for documenting procedures within the `tell-me-go` project to ensure clarity, consistency, and maintainability.

---

### Prerequisites
- Knowledge of Markdown syntax.
- Access to the `SOP/` directory.

---

### 1. Structure of an SOP

#### File Criticality Levels
To ensure stability, files are categorized by risk:
- **Core Entry Points** (`cmd/**/*.go`): **HIGH RISK**. These are the orchestrators and CLI entry points. Modifications require full build verification and syntax validation as per [Self-Update Safety](./self_update_safety.md).
- **Internal/Package Logic** (`internal/**/*.go`, `pkg/**/*.go`): **MEDIUM RISK**. These contain the bulk of the system logic (API handling, history management, metrics). Unit tests must pass before any changes are committed.
- **Configurations/Assets** (`configs/*.yaml`): **LOW RISK**. Ensure valid YAML syntax.

Every SOP should follow a consistent Markdown structure:

- **Title**: A clear, descriptive title prefixed with "Standard Operating Procedure (SOP):".
- **Objective**: A brief statement explaining what the SOP covers and its purpose.
- **Prerequisites**: Any tools (Go version, dependencies), files, or permissions required before starting.
- **Step-by-Step Instructions**: Numbered sections detailing the process.
- **Code Templates**: Reusable code blocks, Go boilerplate, or command sequences for implementation.
- **Verification/Testing**: How to confirm the procedure was successful (e.g., `go test`).
- **Best Practices**: Tips for edge cases, performance, or Go idiomatic patterns.

### 2. The Creation Process

#### Step 1: Identify the Need
- Create an SOP for any task that is complex (3+ steps), recurring, or critical to system stability (e.g., adding new agent tools, refactoring packages, binary deployment).

#### Step 2: Research & Draft
- Explore the Go codebase to identify the "source of truth."
- Perform the task manually once to verify all steps.
- Draft the content following the structure in Section 1.

#### Step 3: Localization
- **File Path**: Save the file in the `SOP/` directory or an appropriate sub-folder (e.g., `SOP/tools/`).
- **Naming Convention**: Use lowercase with underscores (e.g., `SOP/technical/architecture_and_packages.md`).

#### Step 4: Verification
- Follow your own draft as if you were a new user.
- Fix any ambiguities or missing steps discovered during the walkthrough.
- **CI/Hook Integration**: Ensure the new SOP's verification steps are aligned with the pre-commit requirements defined in [Git Workflow](../standards/git_workflow.md).

### 3. Maintenance & Revision
- **Mandatory State Management**: For any SOP involving 3+ steps or destructive actions, the instructions **MUST** include a "Task Initialization" step as per [CLI Standards](../standards/cli_standards.md):
    - **Anchor Tasking**: The first task MUST be named "SOP Compliance: [filename.md]".
    - **Atomic Turns**: Technical milestones and state updates (Tasks/Scratchpad) must occur in separate turns.
    - **Verification**: The agent must read back the state after updates.
- **Evolution**: When a codebase change (e.g., a Go module update or refactor) breaks a documented procedure, the corresponding SOP **must** be updated immediately.
- **Versioning**: Use Git commit messages to track the "why" behind SOP revisions.
- **Consistency**: Periodically review all files in the `SOP/` tree to ensure they don't contradict each other or standard Go idioms.

---

### 4. Implementation Checklist
- [ ] Is the title clear?
- [ ] Does this impact **Core Entry Points**? (If yes, follow [Self-Update Safety](./self_update_safety.md))
- [ ] Are all Go-specific dependencies listed?
- [ ] Is the logic broken down into digestible numbered steps?
- [ ] Are there Go code examples or command templates?
- [ ] Is the file saved in `SOP/` or a sub-folder?
- [ ] Has the quality been verified? (Run `go test ./...` and `go mod tidy` before committing)
- [ ] Are there logic gaps? If no unit test exists for a new package, add it to the technical debt list in the scratchpad.
- [ ] Does this impact **Testing Standards**?
- [ ] Is the commit message structured according to [Git Workflow](../standards/git_workflow.md)?
- [ ] Has the file been committed and pushed to the repository?

