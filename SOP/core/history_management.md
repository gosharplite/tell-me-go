// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

# Standard Operating Procedure (SOP): History Management

### Objective
To define the standards for managing conversation history in `tell-me-go`, ensuring session persistence, valid role alternation for API compliance, and efficient pruning to remain within model context limits.

---

### Prerequisites
- Go toolchain 1.21+.
- `SOP/core/architecture_and_packages.md` (defining the `internal/history` package).
- `internal/api` (for the `Content` and `Part` struct definitions).

---

### Step-by-Step Instructions

#### 1. Data Structure
History must be stored as a slice of `Content` structs, mirroring the Gemini API's `contents` field.
- **File Format**: Standard JSON.
- **Locality**: History files should be saved in the `output/` directory by default, named `last-<MODE>.json`.
- **Serialization**: The `Role` field in the JSON payload must **never** be omitted. Strict providers like Vertex AI will reject payloads where the `role` key is missing. Ensure the struct tag does not use `omitempty` for this field.

#### 2. Role Alternation Rules
The history must strictly alternate between roles to satisfy Vertex AI requirements:
- **First Message**: Must be `user`.
- **Subsequent Messages**: Must alternate `user` -> `model` -> `user` -> `model`.
- **Function Responses**: When the model requests a function call, the results must be sent back as a `functionResponse` part within a `user` role content. This ensures the `user` -> `model` alternation is maintained even during tool execution loops.
- **Validation**: Before sending to the API, the `internal/history` package must verify this sequence. Two consecutive messages with the same role are forbidden.

#### 3. Persistence
- **Loading**: On startup, the system should attempt to load the history file corresponding to the active `MODE`.
- **Saving**: The history must be updated atomically on disk after every successful model response.
- **Atomic Writing**: Use a `.tmp` file and `os.Rename` to prevent corruption.

#### 4. Pruning Logic
To prevent the payload from exceeding the model's context window or the user's budget:
- **Turn Limit**: Defined by `MAX_TURNS` in the config.
- **Action**: When the limit is reached, the oldest pairs (one `user` and one `model` message) must be removed until the count is within limits.
- **System Notice**: If history is pruned, a notification should be added to the scratchpad or logged.

---

### Package Standards
- **Location**: `internal/history`
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

