# ADR-008: Bubble Tea Interactive History Browser

## Status
Proposed (2026-02-15)

## Context
The current `tell-me-go` CLI provides limited history viewing capabilities through the `-l N` flag, which dumps the last N messages to stdout. This approach has several limitations:

1. **Static Output**: History is displayed as a one-time text dump without interactivity
2. **Thought Visibility**: AI reasoning chains (`IsThought: true`) are hidden by default (controlled by `SHOW_THOUGHTS` config)
3. **Poor Large History Handling**: Long conversations become overwhelming wall-of-text output
4. **No Search/Navigation**: Users cannot search within history or jump to specific turns
5. **Limited User Experience**: No keyboard shortcuts, scrolling, or filtering capabilities

However, the architecture already stores complete conversation history with AI thoughts preserved in the `history.jsonl` file, and the `HistoryManager` interface provides full access to this data. The system also already uses the Charm ecosystem (`glamour`, `lipgloss`) for markdown rendering, making Bubble Tea a natural extension.

## Decision
We will implement a new `tell-me-go browse` subcommand using the Bubble Tea TUI framework to provide interactive history exploration. This will be implemented in three phases:

### Phase 1: Core Browser (MVP)
1. **New Subcommand**: `tell-me-go browse` launches interactive TUI
2. **Basic Navigation**: ↑/↓ scroll through messages, spacebar toggle thoughts
3. **Color-Coded Display**: User (blue), Model (green), Thoughts (gray) using existing Lip Gloss styles
4. **TTY Awareness**: Graceful fallback to text output when not in interactive terminal

### Phase 2: Enhanced Features
5. **Search**: `/` to search within history, `n`/`N` navigate matches
6. **Turn Navigation**: Jump to specific turn by number
7. **Tool Call Expansion**: Expand/collapse detailed tool calls and responses
8. **Raw JSON View**: Debug view of underlying data structure

### Phase 3: Advanced Integration
9. **Direct Manipulation**: Pin/unpin turns from TUI using existing `SetPinned` API
10. **Rollback Interface**: Rollback turns directly from browser using `RollbackTurns`
11. **Multi-session Support**: Switch between different mode histories
12. **Export Capability**: Export selected turns to markdown/text files

### Architectural Integration
- **CLI Registration**: Add `browse` command to existing registry in `internal/cli/registry.go`
- **Dependency Reuse**: Leverage existing `ChatService`, `HistoryManager`, and configuration loading
- **TTY Detection**: Use same TTY detection as current UI layer for graceful fallback
- **Configuration Respect**: TUI defaults respect `SHOW_THOUGHTS` config but allow runtime override

### Technical Implementation
```go
// New command structure (internal/cli/browse_command.go)
type browseCommand struct {
    ChatService agent.ChatService  // Reuse existing service
    HomeDir     string
    // ... other existing dependencies
}

// TUI Model (internal/ui/tui/browser.go)  
type HistoryBrowser struct {
    history      []*llm.Content    // From HistoryManager.GetWindow()
    cursor       int
    showThoughts bool              // Toggle via spacebar
    tea.Program                    // Bubble Tea integration
}
```

## Consequences

### Positive
1. **Enhanced Debugging**: Developers can explore AI reasoning chains and tool execution flows
2. **Better UX for Long Conversations**: Paginated, searchable interface replaces overwhelming text dumps
3. **Architectural Alignment**: Builds on existing Charm ecosystem and clean architecture
4. **Backward Compatibility**: `-l N` remains unchanged for scripts/automation
5. **Professional Polish**: Elevates `tell-me-go` from utility to professional-grade tool
6. **Minimal Dependency Impact**: Adds only `bubbletea` and `bubbles` (~2MB binary increase)

### Negative
1. **TTY Requirement**: Bubble Tea requires interactive terminal (with graceful fallback)
2. **Dependency Increase**: Adds ~2MB to binary size (25MB → 27MB)
3. **Learning Curve**: New keybindings for users (mitigated with help screen)
4. **Testing Complexity**: TUI components require different testing approach than CLI

### Risks & Mitigations
- **Risk**: Bubble Tea conflicts with existing event loop
  - **Mitigation**: Isolate TUI to subcommand only; don't mix with main chat flow
- **Risk**: Performance with massive histories
  - **Mitigation**: Lazy loading via `GetWindow()`; paginate display
- **Risk**: Users expect TUI when piping output
  - **Mitigation**: Automatic fallback to text rendering when not in TTY

## Implementation Notes
The implementation leverages existing architectural components:
- **History Storage**: Thoughts already persisted with `IsThought: true` in `history.jsonl`
- **Data Access**: `HistoryManager.GetWindow()` provides full history access
- **UI Consistency**: Uses same color system and styling as existing markdown rendering
- **Configuration**: Respects `SHOW_THOUGHTS` from YAML config while allowing runtime toggle

## User Documentation
For user-facing documentation, mockups, and usage examples, see [docs/user/history-browser.md](../user/history-browser.md).

This decision aligns with the project's clean architecture while significantly improving the user experience for history exploration and debugging.