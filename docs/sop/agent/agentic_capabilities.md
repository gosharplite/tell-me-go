<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->


# Standard Operating Procedure (SOP): Agentic Capabilities (Function Calling)

### Objective
To define the standards for implementing and executing tools (function calling) in `tell-me-go`, ensuring safety, reliability, and strict compliance with Gemini API multi-turn requirements.

---

### Prerequisites
- Go toolchain 1.25+.
- `docs/sop/technical/history_management.md` (defining role alternation).
- Vertex AI support for `functionDeclarations`.

---

### Step-by-Step Instructions

#### 1. Tool Definition (Schema)
Every tool must be defined using a standard JSON schema that matches the Gemini API requirements.
- **Location**: Tool definitions are registered within subpackages of `internal/tools`.
- **Categories**:
    - **Workspace** (`internal/tools/workspace`): `list_files`, `read_file`, `write_file`, `replace_text`, etc.
    - **Analysis** (`internal/tools/analysis`): `find_usages`, `get_type_info`, `get_semantic_diff`, `rename_symbol`, `generate_mermaid_diagram`, etc.
    - **Developer** (`internal/tools/developer`): `run_tests`, `run_linter`, `go_tidy`, `get_coverage`, `check_vulnerabilities`, `verify_architecture`, etc.
    - **Integrations** (`internal/tools/integrations`): `send_teams_message`, Atlassian Stack (`jira_search_issues`, `confluence_read`, etc.), Azure DevOps (`ado_list_pull_requests`, etc.), and Network tools (`http_request`, `read_external_docs`).
    - **State/System**: `get_session_info`, `manage_scratchpad`, `manage_tasks`, `manage_config`, `summarize_history`, `manage_history`, and security tools (`register_safepath`, `bypass_confirmation`).
- **Validation**: Every tool must have a `description` and a clear `parameters` schema using `genai.FunctionDeclaration`.

#### 2. The Orchestration Loop
The CLI must transition from a "One-Shot" call to a "Multi-Turn" loop when tools are involved:
1.  **Send Request**: Send history + tool definitions to Gemini.
2.  **Detect Function Call**: Check if the `candidates[0].content.parts` contains one or more `functionCall` items.
3.  **Parallel Execution**:
    - Multiple `functionCall` parts should be executed in parallel using goroutines and a worker pool (semaphore) to limit concurrency (e.g., `maxConcurrentTools`).
    - **Resource Safety**: The semaphore token MUST be acquired **before** spawning the goroutine to prevent resource exhaustion from an unbounded number of goroutines.
    - **UI Sequencing**: To prevent log interleaving during interactive prompts, all `[Tool Action]` headers must be printed sequentially to `stderr` **before** the parallel goroutines are started.
    - Map the `functionCall.name` to a local Go function.
    - Parse arguments based on the schema.
    - Execute the function and capture the output (success or error).
    - **Context & Timeouts**: Tool execution MUST respect the parent `context.Context` (propagated from the CLI entry point). Apply a `context.WithTimeout` (default 30s) as a child context for non-interactive tools. Interactive tools (e.g., `ask_user`, `execute_command`) are exempt from timeouts but MUST still respect the parent context cancellation (e.g., on `SIGINT`).
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
- **Persistence Request**: These warnings should explicitly instruct the AI to use `manage_scratchpad` and `manage_tasks` to save its work before execution is cut off. Note that these tools are **scoped to the current configuration MODE** (e.g., `output/<MODE>/tasks.json`), allowing for context isolation.
- **Volatile Injection**: Injected warnings must only exist in the API payload and **must not** be saved to the persistent history file on disk to prevent "context rot."

#### 4. Security and Safety
- **Thread Safety**: Since tools are executed in parallel, any tool that modifies shared state (e.g., `manage_tasks`, `manage_scratchpad`) MUST use synchronization primitives like `sync.Mutex` to prevent race conditions.
- **Atomic Writes**: Tools that update files MUST use an atomic write pattern (writing to a temporary file and then renaming it) to ensure file integrity in case of crashes or concurrent access.
- **Robust Parsing**: Tools that load persistent state (JSON, YAML) MUST handle corruption gracefully, typically by resetting to a clean state or attempting partial recovery, to prevent the agent from becoming stuck due to a malformed file.
- **Path Sanitization**: Every tool accessing the filesystem MUST call `tools.IsPathSafe(path)`. This function MUST:
    - Call `filepath.Clean` to resolve traversal attempts.
    - Call `filepath.EvalSymlinks` to prevent symlink-based boundary escapes.
- **Confirmation Gates**: Tools that perform destructive actions (`write_file`, `replace_text`) or system execution (`execute_command`) MUST implement the `confirmDestructiveAction` handshake.
    - The `bypass_confirmation` tool allows the user (via the AI) to disable these prompts for the duration of the **current session**. This state is persistent until the session ends or `revoke_bypass` is called.
- **Interactive Handling**: Tools requiring user input MUST attempt to use `/dev/tty` for interaction to remain functional even when `os.Stdin` is redirected (e.g., in piped command workflows).
- **Error Handling**: Tool execution errors must be caught and sent back to the model as a string response so the model can attempt a fix.

#### 5. Package Structure
- **Location**: `internal/tools`.
- **Registry**: A central map or registry to link function names (strings) to Go implementation functions.
- **Interface**:
    ```go
    type ToolFunc func(ctx context.Context, args map[string]interface{}) (string, error)
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

#### 6. Economic Awareness & Loop Protection (Safe-by-Design)
To prevent runaway costs and infinite execution loops, the agent implements proactive monitoring at the orchestration level.
- **Hard Budget Limit**: An internal USD threshold can be set (programmatically) for the session. The `TurnEngine` performs a pre-turn check; if the current accumulated cost exceeds the limit, execution is immediately halted with `ErrBudgetExceeded`.
- **Loop Detection (SHA-256 Hashing)**: To detect model-level repetition cycles (identical text or repeating tool-call patterns), the agent hashes the **entire JSON model response** using SHA-256. 
    - If the same hash is detected within a window of recent turns, the turn is stopped with an "infinite loop detected" error.
    - **Argument-Level Detection**: Individual tool calls are also tracked by hashing `name + arguments`. If a specific tool is called with identical parameters more than `config.DefaultMaxLoopRepetitions` (e.g., 3 times) within a single session, the loop is broken.
- **Economic Transparency**: Every turn status update includes the current session cost and provides "Price Cliff" warnings when approaching tiered thresholds.

#### 7. Logging & Execution Protocol (Observability)
To ensure maximum observability and traceability in automated refactoring workflows, all mutative tool executions must follow a strict logging protocol.
- **Intent Logging**: Before any tool that modifies the filesystem, executes commands, or changes configurations is called, the agent must explicitly state its **Architectural Intent**.
- **The `[Tool Reason]` Prefix**: 
    - Every mutative tool definition must include a `reason` parameter.
    - The agent must output a text line starting with `[Tool Reason] ` followed by the exact content of the `reason` argument.
    - This line must appear **immediately before** the tool block in the agent's response.
- **UI Display**: The CLI renderer must extract the `reason` argument from `functionCall` objects and display it as a `[Tool Reason]` log line in `stderr` to provide the user with clear context for each action.
