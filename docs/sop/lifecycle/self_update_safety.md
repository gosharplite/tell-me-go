<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->


# Standard Operating Procedure (SOP): Self-Update and Core Modification Safety

### Objective
To define strict safety protocols when the agent is tasked with modifying its own source code or core entry points, preventing the introduction of breaking changes, infinite loops, or uncompilable states.

---

### Prerequisites
- Go toolchain 1.26+.
- [Git Workflow](../standards/git_workflow.md) (for verification chain).

---

### Step-by-Step Instructions

#### 1. Core Entry Point Identification
Core entry points are high-risk files where the main orchestration logic resides:
- `cmd/tell-me-go/main.go`
- `internal/agent/agent.go`
- `internal/agent/turn_engine.go`
- `internal/infrastructure/llm/gemini.go`

#### 2. Modification Workflow (⚠️ CRITICAL)
When modifying a Core Entry Point:
0.  **Initialize Tasks**: Use `manage_tasks` to add a "SOP Compliance: self_update_safety.md" anchor task.
1.  **Read and Analyze**: Perform a full read of the file and its dependencies using `read_file` and `get_package_graph`.
2.  **Incremental Changes**: Apply changes using `replace_text` for precision. Avoid overwriting entire files if possible.
3.  **State Sync (Atomic Turn)**: After the modification, immediately tick the corresponding checklist item in the `task list`.
4.  **Syntax Validation**: Immediately run `go fmt` and `go vet` on the package.
5.  **Compilation Check**: Run `go build ./...`. **If build fails, the agent MUST immediately rollback the change using `git checkout` or by manually reverting the text.**
6.  **Test Verification**: Run `go test -race ./internal/agent/...` (or relevant package).
7.  **Final State Verification**: Read the task list to confirm all safety steps were completed and verified.

#### 3. Protection Against Infinite Loops
If modifying the orchestration loop in `internal/agent/turn_engine.go` or `internal/agent/orchestration`:
- **Timeout Preservation**: Ensure that context timeouts and `MAX_TURNS` logic are not accidentally removed or bypassed.
- **Error Handling**: Verify that new logic does not swallow errors that could trigger a tool-calling recursion.

#### 4. Verification of Agent State
After a successful build of a self-modified binary:
- The agent should verify its own health by running a simple "ping" style command (e.g., `./tell-me-go "echo hello"`) if possible, or by checking the binary's version output.

---

### Verification
1.  **Build PASS**: The project MUST compile after modification.
2.  **Test PASS**: All unit tests MUST pass.
3.  **Linter PASS**: No new high-severity linting errors introduced.

---

### Best Practices
- **Atomic Operations**: One logical change per commit.
- **Comment Preservation**: Do not remove SPDX headers or critical architectural comments.
- **Rollback First**: If in doubt about the impact of a core change, do not proceed without a clear rollback strategy.
