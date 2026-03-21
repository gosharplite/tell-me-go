<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->



# Standard Operating Procedure (SOP): History Management

### Objective
To define the standards for managing conversation history in `tell-me-go`, ensuring session persistence, valid role alternation for API compliance, and efficient pruning to remain within model context limits.

---

### Prerequisites
- Go toolchain 1.26+.
- `docs/sop/technical/architecture_and_packages.md` (defining the `internal/infrastructure/history` package).
- `internal/domain/llm` (for the `Content` and `Part` struct definitions).

---

### Step-by-Step Instructions

#### 1. Data Structure
History must be stored as a slice of `Content` structs, mirroring the Gemini API's `contents` field.
- **File Format**: Standard JSON.
- **Locality**: History files are saved in mode-specific subdirectories under `output/` (e.g., `output/<MODE>/history.json`).
- **Auxiliary State**: Persistent state files (Tasks) must also be scoped to the mode directory (e.g., `output/<MODE>/tasks.json`) to ensure context isolation between different configuration environments.
- **Log Files**: Logs are saved as `tokens.log` within the mode directory for organizational clarity.
- **Serialization**: The `Role` field in the JSON payload must **never** be omitted. Strict providers like Vertex AI will reject payloads where the `role` key is missing. Ensure the struct tag does not use `omitempty` for this field.

#### 2. Role Alternation Rules
The history must strictly alternate between roles to satisfy Vertex AI requirements:
- **First Message**: Must be `user`.
- **Subsequent Messages**: Must alternate `user` -> `model` -> `user` -> `model`.
- **Function Responses**: When the model requests a function call, the results must be sent back as a `functionResponse` part within a `user` role content. This ensures the `user` -> `model` alternation is maintained even during tool execution loops.
- **Validation**: Before sending to the API, the `internal/infrastructure/history` package must verify this sequence. Two consecutive messages with the same role are forbidden.

#### 3. Persistence
- **Loading**: On startup, the system should attempt to load the history file corresponding to the active `MODE`.
- **Saving**: The history must be updated atomically on disk after every successful model response.
- **Atomic Writing**: Use a `.tmp` file and `os.Rename` to prevent corruption.

#### 4. History Maintenance (Self-Healing)
To prevent context overflow and stay within the model's high-performance/low-cost context tiers:
- **Token-Triggered Maintenance**: When the estimated payload exceeds **90%** of `MAX_HISTORY_TOKENS`, the system triggers an automated "Self-Healing" maintenance cycle.
- **Summarization (`summarize_history`)**: Instead of aggressive deletion, the system identifies the oldest **unpinned** turns and uses the model to generate a concise semantic summary. This summary is then injected as a `user` role message, replacing the original turns.
- **Pinning (`manage_history`)**: Users or the agent can **pin** critical instructions or key conversation turns to protect them from summarization or pruning.
- **Turn-Limit Pruning**: If the number of turns exceeds `MAX_HISTORY_TURNS`, the oldest unpinned turns are removed to maintain performance.
- **Consistent Alternation**: Maintenance logic must always ensure the resulting history maintains the `user` -> `model` role alternation.
- **System Notice**: If maintenance cannot reduce the context size sufficiently (due to too many pinned turns), an **URGENT SYSTEM NOTICE** is injected, instructing the model to persist state to the task list and stop before a hard rollback occurs.

#### 5. Volatile vs. Persistent Context
The history manager must distinguish between data that belongs in the permanent session record and data that is temporary for safety.
- **Persistent**: User prompts, Model responses, and Tool outputs. These are saved to disk.
- **Volatile**: System-injected warnings (e.g., "You have 1 turn left"). These must be injected into the API payload but **filtered out** before saving to disk to prevent polluting future conversation turns with stale warnings.

---

### Package Standards
- **Location**: `internal/infrastructure/history`
- **Responsibilities**:
    - Loading/Saving JSON history.
    - Appending new turns.
    - Pruning old turns.
    - Providing the full `[]Content` slice to the API client.

---

### Code Templates

#### History Manager Interface:
```go
type Manager interface {
    Load() error
    Save() error
    AddEntry(role, text string)
    GetContents() []Content
    Prune(maxTurns int)
}
```

---

### Verification
1.  **JSON Integrity**: Verify the saved history file is valid JSON using `json.Valid`.
2.  **Alternation Check**: Write a unit test that fails if two `user` roles are appended consecutively.
3.  **Pruning Test**: Verify that the oldest messages are the ones removed during pruning.

---

### Best Practices
- **Thread Safety**: Use a `sync.Mutex` if the History Manager will be accessed by multiple goroutines.
- **Minimalist Storage**: Do not store internal metadata (like timestamps) inside the `contents` array sent to the API; keep that array "clean" for the model.
- **Rollback**: If a save fails, ensure the existing history file is not lost.
