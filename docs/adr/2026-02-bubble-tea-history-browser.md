# ADR-008: Bubble Tea Interactive History Browser

## Status
Implemented (2026-02-25) - *Revised with Architectural Review Findings on Archival & CQRS*

## Context
The current `tell-me-go` CLI provides limited history viewing capabilities through the `-l N` flag, which dumps the last N messages to stdout. This approach has several limitations:

1. **Static Output**: History is displayed as a one-time text dump without interactivity.
2. **Thought Visibility**: AI reasoning chains (`IsThought: true`) are hidden by default.
3. **Poor Large History Handling**: Long conversations become an overwhelming wall-of-text.
4. **No Search/Navigation**: Users cannot search within history or jump to specific turns.

**The Architectural Conflict (Active vs. Archived History):**
ADR-006 introduced History Log Compaction, splitting conversation history into an active in-memory context (`history.jsonl`) and a disk-only archive (`history.archive.jsonl`) to prevent Out-Of-Memory (OOM) crashes. Currently, the `HistoryManager` only exposes the active history. If the UI is built solely on the existing interface, the user will not see older, summarized conversations. Conversely, naively loading the archive into memory for the UI would re-introduce the exact OOM vulnerabilities ADR-006 fixed.

## Decision
We will implement a new `tell-me-go browse` subcommand using the Bubble Tea TUI framework to provide interactive history exploration. To ensure system stability and protect existing summarization logic, this requires a foundational data layer refactor before UI implementation.

### Phase 0: Unified History Data Layer (Strict Prerequisite)
Before any Bubble Tea UI code (`tea.Model`, `tea.Cmd`) is written, the Coder MUST implement and test the Read Model bounded context:

1. **Domain Layer (Read Boundaries & DTOs)**: 
   Create `internal/domain/ports/history_query.go` to define the isolated UI Read Model:
   ```go
   package ports

   import "time"

   // HistoryViewDTO is the immutable read-model for the UI.
   type HistoryViewDTO struct {
       ID             string
       Role           string
       ContentPreview string // Masked/formatted text (e.g., base64 hidden)
       ThoughtProcess string
       IsArchived     bool   // True if sourced from archive (disables Pinning)
       Timestamp      time.Time
   }

   // ArchiveReader provides O(1) unbounded reading of disk history.
   type ArchiveReader interface {
       // ReadPage uses byte-offsets, NOT full memory loading.
       ReadPage(ctx context.Context, limit int, offset int64) ([]HistoryViewDTO, int64, error)
   }

   // UnifiedHistoryProvider stitches active and archived history for the TUI.
   type UnifiedHistoryProvider interface {
       GetHistoryStream(ctx context.Context, limit int, cursor string) ([]HistoryViewDTO, string, error)
   }
   ```

2. **Infrastructure Layer (Archive Adapter)**: 
   Implement the `ArchiveReader` in `internal/infrastructure/history/`. It **must** use `os.File.Seek(offset, io.SeekStart)` to read `history.archive.jsonl` without loading earlier lines into memory. *Verification:* A benchmark test must prove low memory allocation when reading the end of a 50MB dummy file.

3. **Application Layer (Unified Facade)**: 
   Implement the `UnifiedHistoryProvider` to coordinate `ArchiveReader` and `HistoryManager`. It **must** silently drop synthetic "System Auto-Summary" messages during stitching to prevent "double history" rendering in the UI.

### Phase 1: Core Browser (MVP)
3. **New Subcommand**: `tell-me-go browse` launches the interactive TUI.
4. **Basic Navigation**: ↑/↓ scroll through messages asynchronously via `UnifiedHistoryProvider`, spacebar toggle thoughts.
5. **Color-Coded Display**: User (blue), Model (green), Thoughts (gray).
6. **TTY Awareness**: Graceful fallback to text output when not in an interactive terminal.

### Phase 2: Enhanced Features
7. **Search**: `/` to search within history, `n`/`N` navigate matches.
8. **Turn Navigation**: Jump to specific turn by number.
9. **Tool Call Expansion**: Expand/collapse detailed tool calls and responses.

### Phase 3: Advanced Integration
10. **Direct Manipulation**: Pin/unpin turns from TUI using CQRS Command Ports.
11. **Rollback Interface**: Rollback turns directly from browser using CQRS Command Ports.
12. **Export Capability**: Export selected turns to markdown/text files.

### Architectural Integration & Technical Implementation

To prevent TUI "God Objects", synchronous blocking, and token/memory bloat, implementation must strictly adhere to **CQRS (Command Query Responsibility Segregation)** and **Component Composition**.

#### Strict CQRS Boundary
The `UnifiedHistoryProvider` must **only** be injected into the UI tier. The core Agent and `ContextManager` must continue using the existing `ports.HistoryManager`. This ensures that the `summarize_history` tool and LLM context window remain completely bounded to the active history, preventing context limit crashes.

#### Unified Data Layer Edge Cases
The `UnifiedHistoryProvider` must resolve the following edge cases before supplying data to the TUI:
1. **Summary Filtering**: When stitching the archive and active files, the provider must silently filter out the synthetic "System Auto-Summary" messages injected by the Token Gatekeeper, preventing the TUI from displaying double-histories.
2. **Immutable Archive Boundary**: Pinning and Rollback operations rely on the mutable `HistoryManager`. The TUI must visually disable/block these actions for any turns originating from the immutable archive boundary.
3. **Asset Hydration Check**: The TUI must render textual placeholders for `AssetID` (e.g., `[Image Attached: {ID}]`) rather than attempting to decode binary blobs into standard output.

#### Minor UI Enhancements
- **Pinning Pressure Warning**: Since pinned turns cannot be auto-summarized, pinning too many active messages will cause a context limit crash. The TUI should ideally display a warning if the user pins >50% of the active context.
- **Live Reload (EventBus)**: Support optional `events.EventBus` subscriptions to detect file changes and prompt the user to refresh if the AI is generating responses in another terminal.

```go
// 1. Phase 0: Unified Read Model (internal/ui/history_provider.go)
type UnifiedHistoryProvider struct {
    archive ports.ArchiveReader  // Reads history.archive.jsonl (O(1) lazy load via file.Seek)
    active  ports.HistoryReader  // Reads history.jsonl (Fast RAM access)
}

// 2. Scalable Component Composition (internal/ui/tui/browser.go)
type RootBrowserModel struct {
    viewport ui.ViewportModel
    search   ui.SearchModel

    // Injected CQRS Ports (Separation of Concerns)
    historyProvider UnifiedHistoryProvider    // UI Read-Model (Sees Everything)
    cmdService      port.HistoryCommandService // Mutates Active State

    isLoading bool
}

// 3. Non-blocking Asynchronous I/O via tea.Cmd
func (m RootBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case ScrollDownMsg:
        m.isLoading = true
        // NON-BLOCKING: Dispatch disk read to a background goroutine
        return m, fetchNextPageCmd(m.historyProvider, m.cursor) 
    case PageLoadedMsg:
        m.isLoading = false
        m.viewport.SetData(msg.Data)
        return m, nil
    }
    // ...
}
```

## Consequences

### Positive
1. **Complete Visibility**: Users can transparently browse their entire history (active + archived) without OOM risks.
2. **Safe Summarization**: By using CQRS, `summarize_history` and existing context pipelines remain untouched and completely safe from token bloat.
3. **O(1) Data Access**: Upgrading `jsonlStore` with byte-offset indexing (`file.Seek`) eliminates the JSONL O(N) pagination penalty, guaranteeing smooth scrolling.
4. **Enhanced Debugging**: Developers can explore AI reasoning chains and tool execution flows asynchronously.

### Negative
1. **TTY Requirement**: Bubble Tea requires an interactive terminal (with graceful fallback).
2. **Dependency Increase**: Adds ~2MB to binary size (25MB → 27MB).
3. **Architecture Complexity**: Introduces a necessary but complex `UnifiedHistoryProvider` to handle the math of stitching two distinct storage boundaries (archive + active).

### Risks & Mitigations
- **Risk**: Passing the `UnifiedHistoryProvider` into the Agent/LLM Context Pipeline, breaking `summarize_history` and exceeding context limits.
  - **Mitigation**: Enforce CQRS Hexagonal boundaries. The Agent tier must only accept `ports.HistoryManager`.
- **Risk**: Synchronous I/O blocking the UI thread during scrolling (severe input lag).
  - **Mitigation**: Dispatch all disk reads/writes as asynchronous `tea.Cmd` operations. Display loading indicators.
- **Risk**: Re-introducing OOM bloat when reading the archive.
  - **Mitigation**: Implement an in-memory byte-offset index (`[]int64`) for `ArchiveReader`. It must use `file.Seek(offset, io.SeekStart)` to read pages on-demand without loading the file into memory.
- **Risk**: TUI God Object anti-pattern where state, logic, and rendering merge into a massive single struct.
  - **Mitigation**: Decompose the TUI into isolated sub-models (e.g., `viewportModel`, `searchBarModel`).

## Implementation Notes
- **Offset Indexing**: Scan `history.archive.jsonl` once on session startup to populate the `[]int64` offset slice.
- **UI Consistency**: Uses the same color system and styling as existing markdown rendering.
- **Configuration**: Respects `SHOW_THOUGHTS` from YAML config while allowing runtime toggle.

## User Documentation
For user-facing documentation, mockups, and usage examples, see [docs/user/history-browser.md](../user/history-browser.md).

### 4. Immutable DTOs (Data Transfer Objects)
To prevent accidental state mutation and protect the presentation layer from choking on raw data (e.g., large base64 strings or complex tool payloads), the `UnifiedHistoryProvider` will not return pointers to the core `domain.Message` entities.

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
