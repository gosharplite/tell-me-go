<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# Standard Operating Procedure (SOP): Security and Sandbox Standards

### Objective
To define the mandatory security protocols for the `tell-me-go` agent, ensuring that model-driven tool execution remains within safe boundaries and requires user oversight for destructive actions.

---

### Prerequisites
- Go toolchain 1.26+.
- [Agentic Capabilities](../agent/agentic_capabilities.md) (defining tool execution).

---

### 1. Path Validation (Zero-Trust)
All tools interacting with the local filesystem must verify that the requested paths are within authorized boundaries.

*   **Mandatory Helpers**:
    - `tools.IsPathSafe(path)`: Checks if a path is allowed for **reading**. Includes CWD, Temp, Safe Paths, and Read-Only Paths.
    - `tools.IsPathWritable(path)`: Checks if a path is allowed for **writing**. Includes CWD, Temp, and Safe Paths (excludes Read-Only Paths).
*   **Sanitization Steps**:
    1.  **Clean**: Always apply `filepath.Clean(path)` to resolve `..` and `.` segments.
    2.  **Symlink Resolution**: Call `filepath.EvalSymlinks(absPath)` if the file exists. This prevents "Symlink Attacks" where a model tries to escape a directory via a malicious link.
*   **Authorized Roots**:
    - Current Working Directory (CWD).
    - System Temporary Directory (`os.TempDir()`).
    - Explicitly registered "Safe Paths" (Read/Write).
    - Explicitly registered "Read-Only Paths" (Read-Only).

---

### 2. User Confirmation Handshakes
The assistant must never perform destructive or high-risk actions without explicit user approval.

*   **Destructive Tools**: `write_file`, `replace_text`, `remove_safepath`, `remove_readpath`, `rename_symbol`, `move_definition`.
*   **System Tools**: `execute_command`, `pipe_commands`, `git_commit`, `git_create_branch`.
    *   **Auto-Approval Whitelist**: Side-effect-free commands (e.g., `ls`, `grep`, `git status`) are strictly whitelisted and may bypass confirmation.
    *   **Strict Blocking**: All other commands (especially those with shell operators `|`, `;`, `>`) trigger the confirmation gate.
*   **Mechanism**: Use the injected security manager via `Manager.Authorize(action, target, detail)`.
    - **UI Standard**: The prompt and details must be printed to `os.Stderr` using high-visibility colors (Yellow/Red).
    - **Bypass for Tests**: Use the `TELL_ME_MOCK_ANSWER` environment variable to automate confirmations in E2E tests.

---

### 3. Bypass Confirmation (Automation Mode)
For trusted environments or highly repetitive automated tasks, a `bypass_confirmation` tool is available.

*   **Scope**: Disables all interactive security prompts.
*   **Persistence**: The `bypass_confirmation` state is stored in the `settings` table of `tellmego.db`. Unlike conversation history, it is **not cleared** by the `-new` flag, ensuring a consistent developer experience across restarts.
*   **Revocation**: Use the `revoke_bypass` tool to re-enable confirmations.
*   **AI Awareness**: The AI "remembers" it has enabled bypass via chat history. Because the state is persistent, the AI's mental model and the process state will remain in sync across multiple runs (sessions) within the same mode.

### 4. Terminal Interaction (Stdin Independence)
To support piped workflows (e.g., `cat logs.txt | tell-me-go "analyze"`), interactive tools must not rely solely on `os.Stdin`.

*   **Standard**: Use `/dev/tty` for interactive prompts whenever possible.
*   **Fallback**: If `/dev/tty` is unavailable, fall back to `os.Stdin`.
*   **Concurrency**: Use `TerminalMutex` (sync.Mutex) to ensure that only one tool or component is interacting with the terminal at a time.

---

### 5. Self-Protection
The agent must be prevented from modifying its own security configurations.

*   **Configuration Block**: `IsPathSafe` and `IsPathWritable` must explicitly deny read/write access to the active `safePathsFile` and `readOnlyPathsFile` (the JSON files storing persistent authorizations).
*   **Persistent Auth**: Registering a new path via `register_safepath` or `register_readpath` requires a **Double Confirmation** gate.

---

### Verification
1.  **Security Gate E2E**: Run `tests/e2e/e2e_test.go` and ensure `TestSecurityGate` and `TestSymlinkAttack` pass.
2.  **Manual Audit**: Verify that any new tool added to `internal/tools` follows the path validation and confirmation standards.
