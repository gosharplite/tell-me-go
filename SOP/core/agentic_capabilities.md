# Standard Operating Procedure (SOP): Agentic Capabilities (Function Calling)

### Objective
To define the standards for implementing and executing tools (function calling) in `tell-me-go`, ensuring safety, reliability, and strict compliance with Gemini API multi-turn requirements.

---

### Prerequisites
- Go toolchain 1.21+.
- `SOP/core/history_management.md` (defining role alternation).
- Vertex AI and AI Studio support for `functionDeclarations`.

---

### Step-by-Step Instructions

#### 1. Tool Definition (Schema)
Every tool must be defined using a standard JSON schema that matches the Gemini API requirements.
- **Location**: Store tool definitions in `internal/tools/definitions.go` or a separate JSON file.
- **Validation**: Every tool must have a `description` and a clear `parameters` schema.

#### 2. The Orchestration Loop
The CLI must transition from a "One-Shot" call to a "Multi-Turn" loop when tools are involved:
1.  **Send Request**: Send history + tool definitions to Gemini.
2.  **Detect Function Call**: Check if the `candidates[0].content.parts` contains a `functionCall`.
3.  **Local Execution**:
    - Map the `functionCall.name` to a local Go function.
    - Parse arguments based on the schema.
    - Execute the function and capture the output (success or error).
4.  **Update History**: 
    - Append the model's `functionCall` to history (Role: `model`).
    - Append the execution results as a `functionResponse` (Role: `function`).
5.  **Recurse**: Send the updated history back to the model to receive the final answer or another tool call.

#### 3. Security and Safety
- **Read-Only by Default**: Tools that read data (e.g., `read_file`, `list_files`) require no special confirmation.
- **Write-Safe**: Tools that modify the system (e.g., `write_file`, `execute_command`) must have a confirmation mechanism or restricted paths (e.g., only inside the current directory).
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

