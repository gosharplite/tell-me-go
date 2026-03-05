# SOP: AI Agent Interaction Protocol

## Context
To ensure transparency, safety, and architectural alignment, all AI agents interacting with this project must follow the "Explain Before Execute" protocol. This prevents unintended side effects and ensures the human operator maintains control over the execution flow.

## The "Explain Before Execute" Rule
**Before** performing any file modification (`write_file`, `replace_text`, `append_text`) or executing any system command (`execute_command`, `pipe_commands`), the AI agent **MUST** present a step-by-step Action Plan for review.

### Standard Action Plan Template
An acceptable Action Plan should include:

1.  **Discovery & Context**: Files/resources to be inspected to confirm the current state.
2.  **Impact Analysis**: How the proposed changes affect other components (e.g., CI/CD, architectural boundaries, Go modules).
3.  **Step-by-Step Breakdown**: The exact sequence of operations, including file paths and the nature of changes.
4.  **Verification Strategy**: How the success of the task will be validated (e.g., `go test`, `golangci-lint`, `git status`).
5.  **Commit/Summary**: The planned commit message and final report structure.

## Procedure for New Sessions
Upon initialization, the AI agent is expected to:
1.  Review existing architecture documentation and standards in `docs/sop/standards/`.
2.  Acknowledge this AI Agent Interaction Protocol.
3.  Apply the protocol to all subsequent tasks in the session.

## Maintenance
This SOP is maintained at `docs/sop/standards/ai_agent_interaction.md`.
