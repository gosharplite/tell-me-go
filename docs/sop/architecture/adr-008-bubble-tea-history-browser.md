# ADR-008: Interactive History Exploration (Bubble Tea TUI)

## Status
Proposed

## Context
The `tell-me-go` application currently manages conversation history via a `HistoryManager` that aggressively summarizes and archives older messages to maintain strict LLM context limits and prevent OOM errors. While this is necessary for the Agent's performance (Write Model), it restricts the user's ability to browse, search, and review the complete context of long-running sessions.

We need an interactive, professional-grade CLI interface to explore the entire conversation timeline, including both active memory and the archived `history.archive.jsonl` file.

The primary architectural challenge is building a rich Terminal User Interface (TUI) capable of rendering potentially gigabytes of archived history without:
1. Blocking the main UI thread with synchronous disk I/O.
2. Loading the entire archive into memory.
3. Leaking UI-specific formatting requirements (e.g., masking base64 images) into the core Domain entities.
4. Allowing the user to accidentally mutate or corrupt the Agent's active memory state.

## Decision
We will implement an Interactive History Browser using the **Bubble Tea** TUI framework.

To ensure system stability and adherence to Clean Architecture, we mandate a strict **Command Query Responsibility Segregation (CQRS) Boundary** between the Agent's active state and the TUI's read requirements.

### 1. CQRS Boundary: Separation of Concerns
The existing `HistoryManager` acts as the Write Model. It is solely responsible for appending messages, counting tokens, managing context limits, and performing auto-summarization. The TUI **must not** interact with the `HistoryManager` or the active `[]domain.Message` slice directly.

Instead, we will introduce a dedicated Read Model:
*   **`ArchiveReader` (Infrastructure Port):** Provides O(1) byte-offset indexing for safe, paginated reads of the `history.archive.jsonl` file without loading it into memory.
*   **`UnifiedHistoryProvider` (Application Facade):** Stitches the active session history and the archived history seamlessly for the UI. It will silently drop synthetic "System Auto-Summary" messages during stitching to prevent "double history" UX bugs.

### 2. Immutable DTOs (Data Transfer Objects)
To prevent accidental state mutation and protect the presentation layer from choking on raw data (e.g., large base64 strings), the `UnifiedHistoryProvider` will not return pointers to the core `domain.Message` entities.

Instead, it will map all data into a strictly formatted, read-only DTO tailored for the Bubble Tea UI:

```go
// Application Layer (Read Model DTO)
package history

type HistoryViewDTO struct {
    ID             string
    Role           string
    ContentPreview string // Formatted for UI (e.g., base64 images replaced with "[Attached Image: {ID}]")
    ThoughtProcess string // Optional reasoning blocks
    IsArchived     bool   // UI uses this to visually disable the "Pin" or "Rollback" buttons
    Timestamp      time.Time
    ToolCalls      []string // Summarized tool execution names
}
```

The UI components will only receive and render `HistoryViewDTO` values.

### 3. Asynchronous Execution and Component Modularity
*   **Non-Blocking I/O:** All file reads via the `ArchiveReader` must be executed as asynchronous `tea.Cmd` functions to prevent the Bubble Tea event loop from freezing during disk access.
*   **Avoiding God Objects:** The TUI will not be implemented as a single, monolithic `tea.Model`. It must be composed of distinct, isolated components (e.g., `Viewport`, `SearchBar`, `TimelineList`), each handling its own isolated state and `tea.Msg` updates.
*   **Event-Driven Live Reload:** If live-reloading of the history file is implemented, the TUI must use a decoupled filesystem watcher (e.g., `fsnotify`) that translates file changes into immutable `tea.Msg` events. It must not share a Mutex or file descriptor with the Agent.

## Consequences

### Positive
*   **Memory Safety:** The UI can browse massive archives without risking OOM errors or impacting the LLM context window.
*   **Maintainability:** UI formatting logic (like hiding images or thoughts) is isolated in the DTO mapping phase, keeping the core domain clean.
*   **Stability:** The UI cannot accidentally modify the Agent's active state, as it only receives value-type DTOs. The TUI remains responsive due to asynchronous disk I/O.

### Negative
*   **Complexity:** Introduces a secondary data access path (`UnifiedHistoryProvider`) that must be maintained alongside the `HistoryManager`.
*   **Eventual Consistency:** The UI may momentarily lag behind the Agent's actual state if a long-running generation is in progress, requiring manual or event-driven refreshes.
