// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT


# Standard Operating Procedure (SOP): Agentic Capabilities (Function Calling)

### Objective
To define the standards for implementing and executing tools (function calling) in `tell-me-go`, ensuring safety, reliability, and strict compliance with Gemini API multi-turn requirements.

---

### Prerequisites
- Go toolchain 1.24+.
- `SOP/technical/history_management.md` (defining role alternation).
- Vertex AI support for `functionDeclarations`.

---

### Step-by-Step Instructions

#### 1. Tool Definition (Schema)
Every tool must be defined using a standard JSON schema that matches the Gemini API requirements.
- **Location**: Tool definitions are registered within the `internal/tools` package using the `Register*Tools` functions (e.g., `RegisterFileSystemTools`).
- **Validation**: Every tool must have a `description` and a clear `parameters` schema using `genai.FunctionDeclaration`.

#### 2. The Orchestration Loop
The CLI must transition from a "One-Shot" call to a "Multi-Turn" loop when tools are involved:
1.  **Send Request**: Send history + tool definitions to Gemini.
2.  **Detect Function Call**: Check if the `candidates[0].content.parts` contains one or more `functionCall` items.
3.  **Parallel Execution**:
    - Multiple `functionCall` parts should be executed in parallel using goroutines and a worker pool (semaphore) to limit concurrency (e.g., `maxConcurrentTools`).
    - **UI Sequencing**: To prevent log interleaving during interactive prompts, all `[Tool Action]` headers must be printed sequentially to `stderr` **before** the parallel goroutines are started.
    - Map the `functionCall.name` to a local Go function.
    - Parse arguments based on the schema.
    - Execute the function and capture the output (success or error).
    - **Timeouts**: Apply a `context.WithTimeout` (default 30s) to non-interactive tools. Interactive tools (e.g., `ask_user`, `execute_command`) are exempt from timeouts.
4.  **Update History**: 
    - **CRITICAL**: Append the model's full `Content` to history, including all `parts` (e.g., `thought`, `thoughtSignature`, `functionCall`). Stripping reasoning parts will cause subsequent API calls to fail on Vertex AI.
    - Append the execution results as a `functionResponse` inside a `Content` with **Role: `user`** (as required by the GenAI SDK).
5.  **Output Management**: 
    - **Thoughts**: Print model thoughts to `stderr` in gray immediately as they are received.
    - **Text Parts**: If the model response contains text parts along with tool calls, these must be printed to `stdout` **immediately** before proceeding with tool execution. Do not wait for the final response in the multi-turn loop to print intermediate text.
6.  **Multi-Modal Handling**:
    - Tools that produce media (e.g., `read_image`) return a special serialized string (e.g., `MULTI_MODAL_IMAGE|mime|data|message`).
    - The agent must parse this and append a `user` role `Content` containing the raw `Blob` data to the history to enable vision capabilities.
7.  **Recurse**: Send the updated history back to the model to receive the final answer or another tool call.

#### 3. Safety Monitoring & Warning Injection
The agent must monitor resource limits during the orchestration loop to allow the AI to fail gracefully.
- **Turn Limits**: When the current turn count is close to `MAX_TURNS` (e.g., 3, 2, or 1 turns remaining), the agent must inject escalating **System Notices** into the model's volatile history.
- **Token Limits**: When the payload estimate exceeds **90%** or **95%** of `MAX_HISTORY_TOKENS`, the agent must inject an urgent warning.
- **Persistence Request**: These warnings should explicitly instruct the AI to use `manage_scratchpad` and `manage_tasks` to save its work before execution is cut off. Note that these tools are **scoped to the current configuration MODE** (e.g., `tasks_vertex.json`), allowing for context isolation.
- **Volatile Injection**: Injected warnings must only exist in the API payload and **must not** be saved to the persistent history file on disk to prevent "context rot."

#### 4. Security and Safety
- **Path Sanitization**: Every tool accessing the filesystem MUST call `tools.IsPathSafe(path)`. This function MUST:
    - Call `filepath.Clean` to resolve traversal attempts.
    - Call `filepath.EvalSymlinks` to prevent symlink-based boundary escapes.
- **Confirmation Gates**: Tools that perform destructive actions (`write_file`, `replace_text`) or system execution (`execute_command`) MUST implement the `ConfirmDestructiveAction` handshake.
    - The `bypass_confirmation` tool allows the user (via the AI) to disable these prompts for the duration of the **current session**. This state is persistent until the session ends or `revoke_bypass` is called.
- **Interactive Handling**: Tools requiring user input MUST attempt to use `/dev/tty` for interaction to remain functional even when `os.Stdin` is redirected (e.g., in piped command workflows).
- **Error Handling**: Tool execution errors must be caught and sent back to the model as a string response so the model can attempt a fix.

#### 4. Package Structure
- **Location**: `internal/tools`.
- **Registry**: A central map or registry to link function names (strings) to Go implementation functions.
- **Interface**:
    ```go
    type ToolFunc func(args map[string]interface{}) (string, error)
    ```

---

### Verification
1.  **Role Alternation**: Ensure the history sequence remains valid: `user` -> `model (functionCall)` -> `function (functionResponse)` -> `model`.
2.  **Mocking**: Use a mock registry in unit tests to verify the loop logic without running real file system operations.
3.  **Schema Match**: Verify that the generated JSON for `functionResponse` exactly matches the provider's requirements (especially for Vertex AI).

---

### Best Practices
- **Atomic Operations**: Tools should perform one clear action.
- **Clear Descriptions**: The AI's ability to use a tool depends entirely on the clarity of the tool's description.
- **No Side Effects in Tests**: Ensure tool tests use `t.TempDir()`.
